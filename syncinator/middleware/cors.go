package middleware

import (
	"net/http"

	"github.com/rs/cors"
)

type CorsOptions = cors.Options

func Cors(opt CorsOptions) func(next http.Handler) http.Handler {
	c := cors.New(opt)

	return c.Handler
}
