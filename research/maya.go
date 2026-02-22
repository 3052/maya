package maya

import (
   "crypto/tls"
   "net/http"
)

// NewHTTP1OnlyTransport creates a new http.Transport configured to only use
// HTTP/1.1 for TLS connections
func NewHTTP1OnlyTransport() *http.Transport {
   return &http.Transport{
      TLSClientConfig: &tls.Config{
         // This prevents the negotiation of HTTP/2 during the TLS handshake
         // (ALPN)
         NextProtos: []string{"http/1.1"},
      },
   }
}
