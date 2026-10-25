package httpapi

import (
	"net/http"
	"strings"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/execution"
)

type ActivityDetailHandler struct {
	Read func(*http.Request, string) (evidence.OperationDetail, bool, error)
}

func (handler ActivityDetailHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeOperatorError(writer, http.StatusMethodNotAllowed, "INVALID_ARGUMENT", "Activity detail method is not allowed.")
		return
	}
	identity := strings.TrimPrefix(request.URL.Path, "/api/v1/activity/operations/")
	valid := request.URL.RawQuery == "" && request.URL.EscapedPath() == request.URL.Path && !strings.Contains(identity, "/")
	if strings.HasPrefix(identity, "ex_") {
		_, err := execution.ParseExchangeID(identity)
		valid = valid && err == nil
	} else {
		valid = valid && strings.HasPrefix(identity, "mcp_") && len(identity) > 4 && len(identity) <= 256
		for _, character := range identity {
			valid = valid && (character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-')
		}
	}
	if !valid {
		writeOperatorError(writer, http.StatusBadRequest, "INVALID_ARGUMENT", "Activity identity is invalid.")
		return
	}
	if handler.Read == nil {
		writeOperatorError(writer, http.StatusInternalServerError, "INTERNAL", "Activity detail is unavailable.")
		return
	}
	detail, found, err := handler.Read(request, identity)
	if err != nil {
		writeOperatorError(writer, http.StatusInternalServerError, "INTERNAL", "Activity detail is unavailable.")
		return
	}
	if !found {
		writeOperatorError(writer, http.StatusNotFound, "NOT_FOUND", "Activity evidence is not available; absence does not prove no execution.")
		return
	}
	writeOperatorJSON(writer, http.StatusOK, detail)
}
