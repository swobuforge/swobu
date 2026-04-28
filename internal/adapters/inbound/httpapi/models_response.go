package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/ports"
)

type modelsListResponseDTO struct {
	Object string           `json:"object"`
	Data   []modelsEntryDTO `json:"data"`
}

type modelsEntryDTO struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func writeModelsSuccess(w http.ResponseWriter, out requestpath.ListModelsOutput) {
	data := make([]modelsEntryDTO, 0, len(out.Models))
	for _, model := range out.Models {
		data = append(data, modelsEntryDTO{
			ID:      model.ID,
			Object:  "model",
			Created: 0,
			OwnedBy: "swobu",
		})
	}
	resp := modelsListResponseDTO{
		Object: "list",
		Data:   data,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeModelResolutionHeaders(w http.ResponseWriter, metadata ports.ExecuteMetadata) {
	if strings.TrimSpace(metadata.ModelRequested) != "" {
		w.Header().Set("X-Swobu-Model-Requested", strings.TrimSpace(metadata.ModelRequested))
	}
	if strings.TrimSpace(metadata.ModelResolved) != "" {
		w.Header().Set("X-Swobu-Model-Resolved", strings.TrimSpace(metadata.ModelResolved))
	}
	if strings.TrimSpace(metadata.ModelResolutionMode) != "" {
		w.Header().Set("X-Swobu-Model-Resolution", strings.TrimSpace(metadata.ModelResolutionMode))
	}
}
