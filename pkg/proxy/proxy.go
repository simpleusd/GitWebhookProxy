package proxy

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/stakater/GitWebhookProxy/pkg/parser"
	"github.com/stakater/GitWebhookProxy/pkg/providers"
)

type Proxy struct {
	provider     string
	upstreamURL  string
	allowedPaths []string
	secret       string
	httpClient   *http.Client
}

func (p *Proxy) isPathAllowed(path string) bool {
	// All paths allowed
	if len(p.allowedPaths) == 0 {
		return true
	}

	// Check if given passed exists in allowedPaths
	for _, p := range p.allowedPaths {
		if strings.TrimSuffix(strings.TrimSpace(p), "/") ==
			strings.TrimSuffix(strings.TrimSpace(path), "/") {
			return true
		}
	}
	return false
}

func (p *Proxy) redirect(hook *providers.Hook, path string) (*http.Response, error) {
	if hook == nil {
		return nil, errors.New("Cannot redirect with nil Hook")
	}

	// Parse url to check validity
	url, err := url.Parse(p.upstreamURL + path)
	if err != nil {
		return nil, err
	}

	// Assign default scheme as http if not specified
	if url.Scheme == "" {
		url.Scheme = "http"
	}

	// Create Redirect request
	// TODO: take method as param from original request
	req, err := http.NewRequest(hook.RequestMethod, url.String(), bytes.NewBuffer(hook.Payload))

	if err != nil {
		return nil, err
	}

	if val, ok := hook.Headers[providers.ContentTypeHeader]; ok {
		req.Header.Add(providers.ContentTypeHeader, val)
	}

	// Set Headers from hook
	for key, value := range hook.Headers {
		req.Header.Add(key, value)
	}

	return p.httpClient.Do(req)

}

func (p *Proxy) proxyRequest(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	log.Printf("Proxying Request from '%s', to upstream '%s'\n", r.URL, p.upstreamURL+r.URL.Path)

	if !p.isPathAllowed(r.URL.Path) {
		log.Printf("Not allowed to proxy path: '%s'", r.URL.Path)
		http.Error(w, "Not allowed to proxy path: '"+r.URL.Path+"'", http.StatusForbidden)
		return
	}

	provider, err := providers.NewProvider(p.provider, p.secret)
	if err != nil {
		log.Printf("Error creating provider: %s", err)
		http.Error(w, "Error creating Provider", http.StatusInternalServerError)
		return
	}

	hook, err := parser.Parse(r, provider)
	if err != nil {
		log.Printf("Eror Parsing Hook: %s", err)
		http.Error(w, "Error parsing Hook: "+err.Error(), http.StatusBadRequest)
		return
	}

	if !provider.Validate(*hook) {
		log.Printf("Eror Validating Hook: %v", err)
		http.Error(w, "Error validating Hook", http.StatusBadRequest)
		return
	}

	resp, errs := p.redirect(hook, r.URL.Path)
	if errs != nil {
		log.Printf("Error Redirecting '%s' to upstream '%s': %s\n", r.URL, p.upstreamURL+r.URL.Path, errs)
		http.Error(w, "Error Redirecting '"+r.URL.String()+"' to upstream '"+p.upstreamURL+r.URL.Path+"'", http.StatusInternalServerError)
		return
	}

	if resp.StatusCode >= 400 {
		log.Printf("Error Redirecting '%s' to upstream '%s', Upstream Redirect Status: %s\n", r.URL, p.upstreamURL+r.URL.Path, resp.Status)
		http.Error(w, "Error Redirecting '"+r.URL.String()+"' to upstream '"+p.upstreamURL+r.URL.Path+"' Upstream Redirect Status:"+resp.Status, resp.StatusCode)
		return
	}

	log.Printf("Redirected incomming request '%s' to '%s' with Response: '%s'\n",
		r.URL, p.upstreamURL+r.URL.Path, resp.Status)
}

// Health Check Endpoint
func (p *Proxy) health(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	w.WriteHeader(200)
	w.Write([]byte("I'm Healthy and I know it! ;) "))
}

func (p *Proxy) Run(listenAddress string) error {
	if len(strings.TrimSpace(listenAddress)) == 0 {
		panic("Cannot create Proxy with empty listenAddress")
	}

	router := httprouter.New()
	router.GET("/health", p.health)
	router.POST("/*path", p.proxyRequest)

	log.Printf("Listening at: %s", listenAddress)
	return http.ListenAndServe(listenAddress, router)
}

func NewProxy(upstreamURL string, allowedPaths []string,
	provider string, secret string) (*Proxy, error) {
	// Validate Params
	if len(strings.TrimSpace(secret)) == 0 {
		return nil, errors.New("Cannot create Proxy with empty secret")
	}
	if len(strings.TrimSpace(upstreamURL)) == 0 {
		return nil, errors.New("Cannot create Proxy with empty upstreamURL")
	}
	if len(strings.TrimSpace(provider)) == 0 {
		return nil, errors.New("Cannot create Proxy with empty provider")
	}
	if allowedPaths == nil {
		return nil, errors.New("Cannot create Proxy with nil allowedPaths")
	}

	return &Proxy{
		provider:     provider,
		upstreamURL:  upstreamURL,
		allowedPaths: allowedPaths,
		secret:       secret,
		httpClient: &http.Client{
			Timeout: time.Second * 60,
		},
	}, nil
}
