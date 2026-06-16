package plugin

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/pkg/response"
)

// Request wraps the incoming HTTP request context for the plugin proxy.
type Request struct {
	method      string
	path        string
	queryParams map[string]string
	bodyParams  map[string]interface{}
	pathParams  map[string]string
}

func (r Request) Method() string { return r.method }
func (r Request) Path() string   { return r.path }

// Query retrieves a query parameter value.
func (r Request) Query(key string) string {
	return r.queryParams[key]
}

// Param retrieves a path parameter value.
func (r Request) Param(key string) string {
	return r.pathParams[key]
}

// Body retrieves a JSON body field as string.
func (r Request) Body(key string) string {
	val, ok := r.bodyParams[key]
	if !ok {
		return ""
	}
	return fmt.Sprintf("%v", val)
}

// BodyMap returns the full JSON body as a map.
func (r Request) BodyMap() map[string]interface{} {
	return r.bodyParams
}

// Response represents the HTTP response returned by the plugin handler.
type Response struct {
	Status int
	JSON   interface{}
}

// PluginRoute represents a route registered by a plugin.
type PluginRoute struct {
	PluginSlug string
	Method     string
	Path       string
	Handler    func(ctx context.Context, req Request) Response
}

// PluginProxyRouter routes HTTP requests directly to Go native or WASM plugins dynamically.
type PluginProxyRouter struct {
	routes map[string]PluginRoute
	mu     sync.RWMutex
}

// DefaultProxyRouter is the global singleton proxy router.
var DefaultProxyRouter = NewPluginProxyRouter()

func NewPluginProxyRouter() *PluginProxyRouter {
	return &PluginProxyRouter{
		routes: make(map[string]PluginRoute),
	}
}

// RegisterRoute registers a plugin route pattern (e.g. GET "/items").
func (r *PluginProxyRouter) RegisterRoute(slug, method, path string, handler func(ctx context.Context, req Request) Response) {
	r.mu.Lock()
	defer r.mu.Unlock()

	method = strings.ToUpper(method)
	trimmedPath := strings.Trim(path, "/")
	key := fmt.Sprintf("%s:%s/%s", method, slug, trimmedPath)

	r.routes[key] = PluginRoute{
		PluginSlug: slug,
		Method:     method,
		Path:       path,
		Handler:    handler,
	}
}

// ServeHTTP acts as the single Gin gateway handler routing calls to individual plugin handlers.
func (r *PluginProxyRouter) ServeHTTP(c *gin.Context) {
	slug := c.Param("slug")
	pathParam := c.Param("path") // e.g., "/items" or "/pin/123"

	method := c.Request.Method
	trimmedPath := strings.Trim(pathParam, "/")

	r.mu.RLock()
	var matchedRoute *PluginRoute
	var pathParams map[string]string

	for key, route := range r.routes {
		routeKeyParts := strings.SplitN(key, ":", 2)
		if len(routeKeyParts) != 2 {
			continue
		}
		routeMethod := routeKeyParts[0]
		routePathPattern := routeKeyParts[1]

		if routeMethod != method {
			continue
		}

		targetPath := fmt.Sprintf("%s/%s", slug, trimmedPath)
		if ok, params := matchPath(routePathPattern, targetPath); ok {
			matchedRoute = &route
			pathParams = params
			break
		}
	}
	r.mu.RUnlock()

	if matchedRoute == nil {
		response.Error(c, http.StatusNotFound, "ROUTE_NOT_FOUND", fmt.Sprintf("Plugin route not found: %s %s", method, pathParam))
		return
	}

	queryParams := make(map[string]string)
	for k, v := range c.Request.URL.Query() {
		if len(v) > 0 {
			queryParams[k] = v[0]
		}
	}

	bodyParams := make(map[string]interface{})
	if c.Request.ContentLength > 0 {
		_ = c.ShouldBindJSON(&bodyParams)
	}

	req := Request{
		method:      method,
		path:        pathParam,
		queryParams: queryParams,
		bodyParams:  bodyParams,
		pathParams:  pathParams,
	}

	resp := matchedRoute.Handler(c.Request.Context(), req)
	if resp.JSON != nil {
		c.JSON(resp.Status, resp.JSON)
	} else {
		c.Status(resp.Status)
	}
}

func matchPath(pattern, path string) (bool, map[string]string) {
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	if len(patternParts) != len(pathParts) {
		return false, nil
	}

	params := make(map[string]string)
	for i := 0; i < len(patternParts); i++ {
		pPart := patternParts[i]
		pathPart := pathParts[i]

		if strings.HasPrefix(pPart, ":") {
			paramName := pPart[1:]
			params[paramName] = pathPart
		} else if pPart != pathPart {
			return false, nil
		}
	}

	return true, params
}
