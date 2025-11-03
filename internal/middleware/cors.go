package middleware

import (
	"net/http"
	"strings"

	"dynamic-gateway/internal/config"
)

// CORS middleware
func CORS(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if cfg.AllowAllOrigin {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if len(cfg.AllowedOrigins) > 0 {
				for _, allowed := range cfg.AllowedOrigins {
					if origin == allowed {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						break
					}
				}
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")

			if len(cfg.AllowedHeaders) > 0 {
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.AllowedHeaders, ", "))
			} else {
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			}

			w.Header().Set("Access-Control-Max-Age", "3600")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
