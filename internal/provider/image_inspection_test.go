package provider

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestInspectImageAppliesOneContractToBytes(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	got, err := InspectImage(canonical.ImageMediaPNG, data.Bytes(), DefaultMediaLimits())
	if err != nil || got.Width != 3 || got.Height != 2 {
		t.Fatalf("inspection = %#v err=%v", got, err)
	}
	if _, err := InspectImage(canonical.ImageMediaJPEG, data.Bytes(), DefaultMediaLimits()); err == nil {
		t.Fatal("contradictory declaration accepted")
	}
}

func TestInspectImageRejectsAnimatedGIF(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	frame := image.NewPaletted(image.Rect(0, 0, 1, 1), palette)
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{0, 0}}); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectImage(canonical.ImageMediaGIF, data.Bytes(), DefaultMediaLimits()); err == nil {
		t.Fatal("animated GIF accepted")
	}
}
