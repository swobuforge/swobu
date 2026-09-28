package httpapi

import "net/http"

func writeOperatorError(w http.ResponseWriter, status int, code, message string) {
    writeOperatorJSON(w, status, struct {
        Code string `json:"code"`
        Message string `json:"message"`
    }{Code: code, Message: message})
}
