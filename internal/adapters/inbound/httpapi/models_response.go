package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/routing"
)

type modelsListResponseDTO struct {
	Object string           `json:"object"`
	Data   []modelsEntryDTO `json:"data"`
}

type modelsEntryDTO struct {
	Name    string `json:"name"`
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func writeModelsSuccess(w http.ResponseWriter, out exchange.ListModelsOutput) {
	data := make([]modelsEntryDTO, 0, len(out.Models)+1)
	if out.DefaultModelID != "" {
		data = append(data, modelsEntryDTO{
			Name:    routing.PublicDefaultRouteID,
			ID:      routing.PublicDefaultRouteID,
			Object:  "model",
			Created: 0,
			OwnedBy: "swobu",
		})
	}
	for _, model := range out.Models {
		data = append(data, modelsEntryDTO{
			Name:    model.ID,
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

func writeModelResolutionHeaders(_ http.ResponseWriter) {
}
