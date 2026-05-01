package ingest

import (
	"encoding/json"
	"net/http"

	"github.com/hiveryn/agentruntime"
)

const defaultMaxBodyBytes int64 = 1 << 20

func (r *Receiver) Handler(agent agentruntime.AgentKind) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}

		defer req.Body.Close()
		var payload any
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, defaultMaxBodyBytes)).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		data, err := json.Marshal(payload)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if _, err := r.Ingest(req.Context(), agent, data); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})
}
