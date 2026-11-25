package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}

static int call_host_api(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (stored_host != NULL && stored_host->free_buffer != NULL && ptr != NULL) {
		stored_host->free_buffer(ptr, len);
	}
}
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"
)

const (
	abiVersion       uint32 = 1
	pluginID                = "agy-fingerprint"
	pluginVersion           = "0.1.1"
	defaultUserAgent        = "antigravity/cli/1.5.13 (aidev_client; os_type=linux; arch=amd64; cl=1025641825; auth_method=consumer)"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

type authFileEntry struct {
	AuthIndex string `json:"auth_index"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Type      string `json:"type"`
	Provider  string `json:"provider"`
}

type authListResponse struct {
	Files []authFileEntry `json:"files"`
}

type authGetResponse struct {
	Name string          `json:"name"`
	Path string          `json:"path"`
	JSON json.RawMessage `json:"json"`
}

type managementRequest struct {
	Method string `json:"Method"`
	Path   string `json:"Path"`
}

type applyStatus struct {
	UserAgent   string    `json:"user_agent"`
	LastRun     time.Time `json:"last_run,omitempty"`
	Updated     []string  `json:"updated,omitempty"`
	AlreadySet  int       `json:"already_set"`
	LastError   string    `json:"last_error,omitempty"`
	AuthScanned int       `json:"auth_scanned"`
}

var (
	statusMu sync.Mutex
	status   = applyStatus{UserAgent: defaultUserAgent}
)

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(abiVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required"))
		return 1
	}
	var rawRequest []byte
	if request != nil && requestLen > 0 {
		rawRequest = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, errHandle := handleMethod(C.GoString(method), rawRequest)
	if errHandle != nil {
		writeResponse(response, errorEnvelope("plugin_error", errHandle.Error()))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, length C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = length
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		userAgent := defaultUserAgent
		if len(request) > 0 {
			var lifecycle lifecycleRequest
			if err := json.Unmarshal(request, &lifecycle); err != nil {
				return nil, fmt.Errorf("decode lifecycle request: %w", err)
			}
			userAgent = configuredUserAgent(lifecycle.ConfigYAML)
		}
		if err := applyFingerprint(userAgent); err != nil {
			return nil, err
		}
		return okEnvelope(registration())
	case "management.register":
		return okEnvelope(map[string]any{
			"routes": []map[string]string{{
				"Method":      http.MethodPost,
				"Path":        "/plugins/agy-fingerprint/apply",
				"Description": "Reapplies the configured agy User-Agent to every Antigravity credential.",
			}},
			"resources": []map[string]string{{
				"Path":        "/status",
				"Menu":        "AGY Fingerprint",
				"Description": "Shows and reapplies the official agy User-Agent fingerprint on Antigravity credentials.",
			}},
		})
	case "management.handle":
		var req managementRequest
		if len(request) > 0 {
			if err := json.Unmarshal(request, &req); err != nil {
				return nil, fmt.Errorf("decode management request: %w", err)
			}
		}
		if req.Method == http.MethodPost && strings.HasSuffix(req.Path, "/apply") {
			statusMu.Lock()
			userAgent := status.UserAgent
			statusMu.Unlock()
			if err := applyFingerprint(userAgent); err != nil {
				return nil, err
			}
		}
		statusMu.Lock()
		body, err := json.Marshal(status)
		statusMu.Unlock()
		if err != nil {
			return nil, err
		}
		return okEnvelope(map[string]any{
			"StatusCode": http.StatusOK,
			"Headers":    map[string][]string{"content-type": {"application/json"}},
			"Body":       body,
		})
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func registration() map[string]any {
	return map[string]any{
		"schema_version": 1,
		"metadata": map[string]any{
			"Name":             pluginID,
			"Version":          pluginVersion,
			"Author":           "Pokemod",
			"GitHubRepository": "https://github.com/esauvisky/CLIProxyAPI-AGY-Fingerprint-Plugin",
			"ConfigFields": []map[string]any{{
				"Name":        "user_agent",
				"Type":        "string",
				"Default":     defaultUserAgent,
				"Description": "User-Agent written to every Antigravity credential. The default matches agy 1.5.13 on linux/amd64.",
			}},
		},
		"capabilities": map[string]bool{"management_api": true},
	}
}

func configuredUserAgent(configYAML []byte) string {
	for _, line := range strings.Split(string(configYAML), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "user_agent:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "user_agent:"))
		value = strings.Trim(value, "\"'")
		if value != "" {
			return value
		}
	}
	return defaultUserAgent
}

func applyFingerprint(userAgent string) error {
	result, err := callHost("host.auth.list", map[string]any{})
	if err != nil {
		recordFailure(userAgent, err)
		return fmt.Errorf("list credentials: %w", err)
	}
	var listed authListResponse
	if err := json.Unmarshal(result, &listed); err != nil {
		recordFailure(userAgent, err)
		return fmt.Errorf("decode credential list: %w", err)
	}

	next := applyStatus{UserAgent: userAgent, LastRun: time.Now().UTC(), Updated: []string{}}
	for _, entry := range listed.Files {
		provider := strings.ToLower(strings.TrimSpace(entry.Provider))
		if provider == "" {
			provider = strings.ToLower(strings.TrimSpace(entry.Type))
		}
		if provider != "antigravity" {
			continue
		}
		next.AuthScanned++
		name, raw, err := readAuth(entry)
		if err != nil {
			recordFailure(userAgent, err)
			return err
		}
		changed, updated, err := injectUserAgent(raw, userAgent)
		if err != nil {
			recordFailure(userAgent, err)
			return fmt.Errorf("patch %s: %w", name, err)
		}
		if !changed {
			next.AlreadySet++
			continue
		}
		if err := writeAuth(entry, name, updated); err != nil {
			recordFailure(userAgent, err)
			return err
		}
		next.Updated = append(next.Updated, name)
	}
	statusMu.Lock()
	status = next
	statusMu.Unlock()
	return nil
}

func readAuth(entry authFileEntry) (string, json.RawMessage, error) {
	if entry.AuthIndex != "" {
		result, err := callHost("host.auth.get", map[string]string{"auth_index": entry.AuthIndex})
		if err != nil {
			return "", nil, fmt.Errorf("read credential %s: %w", entry.Name, err)
		}
		var response authGetResponse
		if err := json.Unmarshal(result, &response); err != nil {
			return "", nil, fmt.Errorf("decode credential %s: %w", entry.Name, err)
		}
		name := response.Name
		if name == "" {
			name = entry.Name
		}
		return name, response.JSON, nil
	}
	if entry.Path == "" {
		return "", nil, fmt.Errorf("credential %s has neither auth_index nor path", entry.Name)
	}
	raw, err := os.ReadFile(entry.Path)
	if err != nil {
		return "", nil, fmt.Errorf("read credential %s: %w", entry.Path, err)
	}
	return entry.Name, raw, nil
}

func writeAuth(entry authFileEntry, name string, raw json.RawMessage) error {
	if entry.AuthIndex != "" {
		_, err := callHost("host.auth.save", map[string]any{"name": name, "json": raw})
		if err != nil {
			return fmt.Errorf("save credential %s: %w", name, err)
		}
		return nil
	}
	if entry.Path == "" {
		return fmt.Errorf("credential %s has no path for direct update", name)
	}
	if err := os.WriteFile(entry.Path, raw, 0o600); err != nil {
		return fmt.Errorf("write credential %s: %w", entry.Path, err)
	}
	return nil
}

func injectUserAgent(raw json.RawMessage, userAgent string) (bool, json.RawMessage, error) {
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return false, nil, fmt.Errorf("invalid credential JSON: %w", err)
	}
	headers := map[string]any{}
	if existing, ok := metadata["headers"].(map[string]any); ok {
		headers = existing
	}
	current, _ := metadata["user_agent"].(string)
	currentHeader, _ := headers["User-Agent"].(string)
	if current == userAgent && currentHeader == userAgent {
		return false, nil, nil
	}
	metadata["user_agent"] = userAgent
	headers["User-Agent"] = userAgent
	metadata["headers"] = headers
	updated, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return false, nil, err
	}
	updated = append(updated, '\n')
	return true, updated, nil
}

func recordFailure(userAgent string, err error) {
	statusMu.Lock()
	status.UserAgent = userAgent
	status.LastRun = time.Now().UTC()
	status.LastError = err.Error()
	statusMu.Unlock()
}

func okEnvelope(result any) ([]byte, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}

func callHost(method string, payload any) (json.RawMessage, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal host callback %s: %w", method, err)
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))
	var response C.cliproxy_buffer
	requestPtr := (*C.uint8_t)(C.CBytes(rawPayload))
	defer C.free(unsafe.Pointer(requestPtr))
	code := C.call_host_api(cMethod, requestPtr, C.size_t(len(rawPayload)), &response)
	var rawResponse []byte
	if response.ptr != nil && response.len > 0 {
		rawResponse = C.GoBytes(response.ptr, C.int(response.len))
	}
	if response.ptr != nil {
		C.free_host_buffer(response.ptr, response.len)
	}
	if len(rawResponse) == 0 {
		return nil, fmt.Errorf("host callback %s returned no response, code=%d", method, int(code))
	}
	var env envelope
	if err := json.Unmarshal(rawResponse, &env); err != nil {
		return nil, fmt.Errorf("decode host callback %s: %w", method, err)
	}
	if !env.OK {
		if env.Error != nil {
			return nil, fmt.Errorf("host callback %s failed: %s", method, env.Error.Message)
		}
		return nil, fmt.Errorf("host callback %s failed", method)
	}
	return env.Result, nil
}
