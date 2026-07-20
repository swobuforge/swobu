package session

import (
	"bytes"
	"fmt"
	"math"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ResolvedMediaAsset is one checked, owned byte asset shared by one or more
// historical request occurrences.
type ResolvedMediaAsset struct {
	mediaType canonical.ImageMediaType
	bytes     []byte
}

func NewResolvedMediaAsset(mediaType canonical.ImageMediaType, data []byte) (ResolvedMediaAsset, error) {
	if mediaType == "" || len(data) == 0 {
		return ResolvedMediaAsset{}, fmt.Errorf("resolved media asset is invalid")
	}
	return ResolvedMediaAsset{mediaType: mediaType, bytes: append([]byte(nil), data...)}, nil
}

func (a ResolvedMediaAsset) MediaType() canonical.ImageMediaType { return a.mediaType }
func (a ResolvedMediaAsset) Bytes() []byte                       { return append([]byte(nil), a.bytes...) }
func (a ResolvedMediaAsset) Clone() ResolvedMediaAsset {
	return ResolvedMediaAsset{mediaType: a.mediaType, bytes: append([]byte(nil), a.bytes...)}
}

type resolvedMediaAssetID uint32

type resolvedMediaBindingRef struct {
	position  canonical.RequestPartRef
	sourceURL string
	asset     resolvedMediaAssetID
}

// ResolvedMedia is checkpoint media with private occurrence bindings and owned
// assets. Bind and Merge normalize identical assets; a future persistence
// decoder must rebuild values through those checked operations.
type ResolvedMedia struct {
	assets   []ResolvedMediaAsset
	bindings []resolvedMediaBindingRef
}

// Bind returns an owned copy with one occurrence bound to an existing or new
// byte asset. A position may have only one durable meaning.
func (m ResolvedMedia) Bind(position canonical.RequestPartRef, sourceURL string, mediaType canonical.ImageMediaType, data []byte) (ResolvedMedia, error) {
	asset, err := NewResolvedMediaAsset(mediaType, data)
	if err != nil {
		return ResolvedMedia{}, err
	}
	out := m.Clone()
	for _, binding := range out.bindings {
		if binding.position == position {
			return ResolvedMedia{}, fmt.Errorf("resolved media position item %d part %d is already bound", position.Item, position.Part)
		}
	}
	assetID := resolvedMediaAssetID(len(out.assets))
	for index, existing := range out.assets {
		if existing.mediaType == asset.mediaType && bytes.Equal(existing.bytes, asset.bytes) {
			assetID = resolvedMediaAssetID(index)
			break
		}
	}
	if int(assetID) == len(out.assets) {
		out.assets = append(out.assets, asset)
	}
	out.bindings = append(out.bindings, resolvedMediaBindingRef{position: position, sourceURL: sourceURL, asset: assetID})
	return out, nil
}

// Resolve returns the asset bound to this exact occurrence and URL spelling.
func (m ResolvedMedia) Resolve(position canonical.RequestPartRef, sourceURL string) (ResolvedMediaAsset, bool) {
	for _, binding := range m.bindings {
		if binding.position != position || binding.sourceURL != sourceURL || int(binding.asset) >= len(m.assets) {
			continue
		}
		return m.assets[binding.asset].Clone(), true
	}
	return ResolvedMediaAsset{}, false
}

// Merge returns normalized evidence containing every non-conflicting binding.
func (m ResolvedMedia) Merge(other ResolvedMedia) (ResolvedMedia, error) {
	out := m.Clone()
	for _, binding := range other.bindings {
		if int(binding.asset) >= len(other.assets) {
			return ResolvedMedia{}, fmt.Errorf("resolved media binding references missing asset")
		}
		asset := other.assets[binding.asset]
		if existing, ok := out.Resolve(binding.position, binding.sourceURL); ok {
			if existing.mediaType != asset.mediaType || !bytes.Equal(existing.bytes, asset.bytes) {
				return ResolvedMedia{}, fmt.Errorf("resolved media binding conflicts at item %d part %d", binding.position.Item, binding.position.Part)
			}
			continue
		}
		var err error
		out, err = out.Bind(binding.position, binding.sourceURL, asset.mediaType, asset.bytes)
		if err != nil {
			return ResolvedMedia{}, err
		}
	}
	return out, nil
}

// ShiftItems moves attempt-local occurrence bindings into their durable
// semantic-request coordinate space while preserving content-part indexes.
func (m ResolvedMedia) ShiftItems(offset uint32) (ResolvedMedia, error) {
	out := m.Clone()
	for index := range out.bindings {
		if math.MaxUint32-out.bindings[index].position.Item < offset {
			return ResolvedMedia{}, fmt.Errorf("resolved media item position overflows semantic coordinates")
		}
		out.bindings[index].position.Item += offset
	}
	return out, nil
}

func (m ResolvedMedia) AssetCount() int   { return len(m.assets) }
func (m ResolvedMedia) BindingCount() int { return len(m.bindings) }

// Validate rejects corrupt persisted references and duplicate occurrence keys.
func (m ResolvedMedia) Validate() error {
	seen := map[canonical.RequestPartRef]bool{}
	referenced := make([]bool, len(m.assets))
	for _, binding := range m.bindings {
		if binding.sourceURL == "" || int(binding.asset) >= len(m.assets) {
			return fmt.Errorf("resolved media binding is invalid")
		}
		if seen[binding.position] {
			return fmt.Errorf("resolved media position is duplicated")
		}
		seen[binding.position] = true
		referenced[binding.asset] = true
	}
	for _, asset := range m.assets {
		if _, err := NewResolvedMediaAsset(asset.mediaType, asset.bytes); err != nil {
			return err
		}
	}
	for index, used := range referenced {
		if !used {
			return fmt.Errorf("resolved media asset %d has no occurrence binding", index)
		}
	}
	return nil
}

// ValidateForRequest additionally proves every binding names the URL at its
// exact durable request-tree occurrence.
func (m ResolvedMedia) ValidateForRequest(request canonical.CanonicalRequest) error {
	if err := m.Validate(); err != nil {
		return err
	}
	urls := map[canonical.RequestPartRef]string{}
	if err := canonical.WalkRequestImages(request, func(position canonical.RequestPartRef, _ canonical.ImagePlacement, image canonical.ImagePart) error {
		if source, ok := image.Source().URL(); ok {
			urls[position] = source.String()
		}
		return nil
	}); err != nil {
		return err
	}
	for _, binding := range m.bindings {
		if urls[binding.position] != binding.sourceURL {
			return fmt.Errorf("resolved media binding does not match request occurrence")
		}
	}
	return nil
}

func (m ResolvedMedia) Clone() ResolvedMedia {
	out := ResolvedMedia{assets: make([]ResolvedMediaAsset, len(m.assets)), bindings: append([]resolvedMediaBindingRef(nil), m.bindings...)}
	for index, asset := range m.assets {
		out.assets[index] = asset.Clone()
	}
	return out
}
