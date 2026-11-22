package handler

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// OpenAPI 只列出独立机器 API，逐项说明请求体、幂等键和认证范围。
func (h *MachineHandler) OpenAPI(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(200, MachineOpenAPI())
}

// MachineOpenAPI 返回与实际机器路由一致的 OpenAPI 3.1 文档。
func MachineOpenAPI() map[string]any {
	schemaRef := func(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	response := map[string]any{"description": "Public JSON response; credentials are never returned", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object"}}}}
	operation := func(path, method, summary, scope, body string, command bool, id bool) map[string]any {
		op := map[string]any{"operationId": machineOperationID(method, path), "summary": summary, "description": "Required capability: " + scope + ". Bearer machine keys only; no browser JWT, NAS key or Cookie fallback.", "security": []any{map[string]any{"machineBearer": []string{}}}, "responses": map[string]any{"200": response, "400": map[string]any{"description": "Invalid body or missing Idempotency-Key"}, "401": map[string]any{"description": "Invalid, expired or revoked machine key"}, "403": map[string]any{"description": "Insufficient capability"}, "409": map[string]any{"description": "Revision or idempotency conflict"}}}
		parameters := []any{}
		if command {
			parameters = append(parameters, map[string]any{"name": "Idempotency-Key", "in": "header", "required": true, "schema": map[string]any{"type": "string", "minLength": 8, "maxLength": 128, "pattern": "^[A-Za-z0-9_.:-]{8,128}$"}, "description": "Durable per-key replay token; same method/path/canonical JSON only. Replays reauthorize. Responses encrypted at rest."})
		}
		if id {
			parameters = append(parameters, map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
		}
		if len(parameters) > 0 {
			op["parameters"] = parameters
		}
		if body != "" {
			op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": schemaRef(body)}}}
		}
		return op
	}
	paths := map[string]any{}
	add := func(path, method, summary, scope, body string, command bool) {
		entry, _ := paths[path].(map[string]any)
		if entry == nil {
			entry = map[string]any{}
			paths[path] = entry
		}
		entry[method] = operation(path, method, summary, scope, body, command, containsID(path))
	}
	add("/api/v1/openapi.json", "get", "Download this machine API contract", "read", "", false)
	add("/api/v1/connection", "get", "Get this key's connection metadata and management entry points", "read", "", false)
	add("/api/v1/dashboard", "get", "Get automation rules, task state and run history", "read", "", false)
	add("/api/v1/rules", "get", "Get public automation rules", "read", "", false)
	add("/api/v1/rules", "put", "Update rules (publish required while enabled/pending, or enabling)", "write; publish when execution is enabled or pending", "Rules", true)
	add("/api/v1/sites", "get", "List all native and imported site profiles", "read", "", false)
	add("/api/v1/sites/schema", "get", "Get declarative profile JSON Schema", "read", "", false)
	add("/api/v1/sites/template", "get", "Get editable profile template", "read", "", false)
	add("/api/v1/sites/{id}/export", "get", "Export public declaration without credentials", "read", "", false)
	add("/api/v1/sites/preview", "post", "Validate a site declaration offline; no site writes", "write", "SiteProfile", true)
	add("/api/v1/sites/import", "post", "Import or explicitly update a profile and native site row", "write; publish while enabled or pending", "ProfileImport", true)
	add("/api/v1/sites/{id}/credentials", "put", "Set site credentials (omitted preserves, empty clears)", "write; publish while enabled or pending", "Credentials", true)
	add("/api/v1/tasks", "get", "List real tasks and quota", "read", "", false)
	add("/api/v1/tasks", "post", "Create an automation run and reserve tasks", "publish", "RunRequest", true)
	add("/api/v1/runs", "get", "List actual run history", "read", "", false)
	add("/api/v1/runs", "post", "Create an automation run and reserve tasks", "publish", "RunRequest", true)
	add("/api/v1/scans", "post", "Preview candidates without quota or download/publish side effects", "write", "PreviewRequest", true)
	add("/api/v1/tasks/{id}/cancel", "post", "Cancel remaining task authorization", "write", "Empty", true)
	add("/api/v1/tasks/{id}/retry", "post", "Retry eligible pre-publish task; never blind upload retry", "publish", "Empty", true)
	add("/api/v1/tasks/{id}/reconcile", "post", "Reconcile an ambiguous result without uploading again", "publish", "Reconcile", true)
	return map[string]any{"openapi": "3.1.0", "info": map[string]any{"title": "STARPT scoped automation API", "version": "1.0.0", "description": "Site health is declared/unverified. Defaults schedule and auto publish disabled. Never edits third-party torrents. Credentials use native-compatible Cookie storage; RSS and idempotency responses encrypted using server machine-registry.key. Keep key with backups."}, "paths": paths, "components": map[string]any{"securitySchemes": map[string]any{"machineBearer": map[string]any{"type": "http", "scheme": "bearer", "bearerFormat": "ptn_ token"}}, "schemas": map[string]any{"SiteProfile": SiteProfileSchema(), "ProfileImport": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"profile"}, "properties": map[string]any{"profile": schemaRef("SiteProfile"), "update": map[string]any{"type": "boolean", "default": false}, "expected_revision": map[string]any{"type": "integer", "minimum": 0, "default": 0}}}, "Credentials": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"cookie": map[string]any{"type": "string", "maxLength": 32768, "writeOnly": true}, "rss": map[string]any{"type": "string", "maxLength": 4096, "writeOnly": true}}}, "Rules": machineRulesSchema(), "PreviewRequest": map[string]any{"anyOf": []any{schemaRef("Rules"), map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"settings": schemaRef("Rules")}}}}, "RunRequest": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"selected_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "enqueue": map[string]any{"type": "boolean", "default": false}}, "description": "Uses saved settings. Explicit selected_ids or enqueue reserves matching eligible candidates even when execution is disabled; requires publish capability."}, "Empty": map[string]any{"type": "object", "additionalProperties": false}, "Reconcile": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"target_url"}, "properties": map[string]any{"target_url": map[string]any{"type": "string", "description": "HTTPS target detail URL. Reconciliation verifies identical payload and target tracker; never causes a new upload."}}}}}}
}
func containsID(path string) bool {
	for i := 0; i+4 <= len(path); i++ {
		if path[i:i+4] == "{id}" {
			return true
		}
	}
	return false
}

// machineOperationID derives stable, distinct client names from the method and route.
func machineOperationID(method, path string) string {
	var id strings.Builder
	id.WriteString(strings.ToLower(method))
	for _, segment := range strings.Split(path, "/") {
		if segment == "" {
			continue
		}
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			segment = "by_" + strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
		}
		for _, word := range strings.FieldsFunc(segment, func(r rune) bool {
			return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
		}) {
			if word == "" {
				continue
			}
			id.WriteString(strings.ToUpper(word[:1]))
			id.WriteString(word[1:])
		}
	}
	return id.String()
}

func machineRulesSchema() map[string]any {
	props := map[string]any{}
	for _, key := range []string{"enabled", "auto_publish", "schedule_enabled", "prefer_official", "target_absent_priority"} {
		props[key] = map[string]any{"type": "boolean"}
	}
	for _, key := range []string{"source_site", "target_site", "downloader_id", "save_path", "schedule_time"} {
		props[key] = map[string]any{"type": "string"}
	}
	props["timezone"] = map[string]any{"type": "string", "const": "Asia/Shanghai"}
	props["schedule_day"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 28}
	props["monthly_limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 100}
	for _, key := range []string{"min_seeders", "min_leechers"} {
		props[key] = map[string]any{"type": "integer", "minimum": 0, "maximum": 100000}
	}
	for _, key := range []string{"min_size_gib", "max_size_gib", "monthly_budget_gib"} {
		props[key] = map[string]any{"type": "number", "exclusiveMinimum": 0, "maximum": 100000}
	}
	props["reserve_space_gib"] = map[string]any{"type": "number", "minimum": 0, "maximum": 100000}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": props, "description": "Partial automation rule update. min_size_gib <= max_size_gib <= monthly_budget_gib; schedule_time HH:mm. Enabling requires existing native downloader/save path and supported source/target profiles."}
}
