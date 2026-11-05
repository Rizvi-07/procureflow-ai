package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"
)

type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, httpCode int, code, message string, retryable bool) {
	writeJSON(w, httpCode, struct {
		Error errorBody `json:"error"`
	}{errorBody{code, message, retryable}})
}

// decodeJSON accepts exactly one object. MaxBytesReader also counts trailing
// whitespace, so a small valid object cannot bypass the overall body limit.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request, limit int64) (*T, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json", false)
		return nil, false
	}
	if encoding := r.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "compressed request bodies are not supported", false)
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "request body exceeds the configured limit", false)
		} else {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "unable to read JSON body", false)
		}
		return nil, false
	}
	if !utf8.Valid(data) {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "JSON must be valid UTF-8", false)
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value *T
	if err := decoder.Decode(&value); err != nil || value == nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "body must be a JSON object with known fields and correctly typed values", false)
		return nil, false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "body must contain exactly one JSON object", false)
		return nil, false
	}
	return value, true
}
