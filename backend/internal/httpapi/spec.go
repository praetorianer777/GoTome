package httpapi

import (
	"cmp"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/openapi"
	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// specVersion is the version of the API, not of the build: the document is
// checked in, and must not change with every release.
const specVersion = "1"

const jsonMedia = "application/json"

// sessionScheme names the security scheme of routes that need a session.
const sessionScheme = "session"

var pathParam = regexp.MustCompile(`\{([^}/]+)\}`)

// Spec is the OpenAPI document of the API, derived from the route table.
func Spec() *openapi.Document {
	b := openapi.NewBuilder()
	b.Descriptions[reflect.TypeOf(APIError{})] = "Why a request failed."
	var permissions []string
	for _, p := range auth.Permissions(auth.RoleAdmin) {
		permissions = append(permissions, string(p))
	}
	b.Enums[reflect.TypeOf(auth.Permission(""))] = permissions
	b.FieldOverrides["Facet.field"] = &openapi.Schema{Type: "string", Enum: []string{
		catalog.FieldAuthor, catalog.FieldSeries, catalog.FieldTag,
		catalog.FieldLanguage, catalog.FieldPublished, catalog.FieldFormat,
	}}
	b.FieldOverrides["SearchHit.match"] = &openapi.Schema{Type: "string", Enum: []string{
		catalog.MatchTitle, catalog.MatchAuthor, catalog.MatchSeries,
	}}
	b.FieldOverrides["SettingView.kind"] = &openapi.Schema{Type: "string", Enum: []string{settings.KindText, settings.KindSecret}}
	roles := &openapi.Schema{Type: "string", Enum: []string{auth.RoleAdmin, auth.RoleEditor, auth.RoleReader}}
	for _, field := range []string{"User.role", "Account.role", "CurrentUser.role", "CreateAccountRequest.role", "UpdateAccountRequest.role"} {
		b.FieldOverrides[field] = roles
	}
	b.FieldOverrides["UploadForm.file"] = &openapi.Schema{Type: "string", Format: "binary"}
	b.FieldOverrides["Uploaded.outcome"] = &openapi.Schema{
		Type: "string", Enum: []string{ingest.UploadAdded, ingest.UploadDuplicate},
	}
	failure := &openapi.Response{
		Description: "The request failed.",
		Content:     map[string]openapi.MediaType{jsonMedia: {Schema: b.SchemaOf(errorEnvelope{})}},
	}

	doc := &openapi.Document{
		OpenAPI: "3.1.0",
		Info: openapi.Info{
			Title:       "GOtome API",
			Version:     specVersion,
			Description: "Every failure is answered with the ErrorEnvelope, whatever the status.",
		},
		Servers: []openapi.Server{{URL: APIPrefix}},
		Paths:   map[string]openapi.PathItem{},
	}

	tags := map[string]bool{}
	for _, rt := range (&Server{}).routes() {
		op := &openapi.Operation{
			OperationID: rt.ID,
			Summary:     rt.Summary,
			Tags:        []string{rt.Tag},
			Responses:   map[string]*openapi.Response{"default": failure},
		}
		op.Permission = string(rt.Permission)
		if rt.Permission != auth.Public {
			op.Security = []map[string][]string{{sessionScheme: {}}}
		}
		for _, m := range pathParam.FindAllStringSubmatch(rt.Path, -1) {
			op.Parameters = append(op.Parameters, openapi.Parameter{
				Name: m[1], In: "path", Required: true, Schema: &openapi.Schema{Type: "string"},
			})
		}
		op.Parameters = append(op.Parameters, queryParameters(rt.Query)...)
		if rt.Request != nil {
			op.RequestBody = &openapi.RequestBody{
				Required: true,
				Content:  map[string]openapi.MediaType{cmp.Or(rt.Consumes, jsonMedia): {Schema: b.SchemaOf(rt.Request)}},
			}
		}
		success := &openapi.Response{Description: http.StatusText(rt.successStatus())}
		switch {
		case rt.Response != nil:
			success.Content = map[string]openapi.MediaType{jsonMedia: {Schema: b.SchemaOf(rt.Response)}}
		case rt.Produces != "":
			success.Content = map[string]openapi.MediaType{
				rt.Produces: {Schema: &openapi.Schema{Type: "string", Format: "binary"}},
			}
		}
		op.Responses[strconv.Itoa(rt.successStatus())] = success

		if doc.Paths[rt.Path] == nil {
			doc.Paths[rt.Path] = openapi.PathItem{}
		}
		doc.Paths[rt.Path][strings.ToLower(rt.Method)] = op
		tags[rt.Tag] = true
	}

	for _, name := range openapi.SortedKeys(tags) {
		doc.Tags = append(doc.Tags, openapi.Tag{Name: name})
	}
	doc.Components.Schemas = b.Components()
	doc.Components.SecuritySchemes = map[string]openapi.SecurityScheme{
		sessionScheme: {
			Type: "apiKey", In: "cookie", Name: SessionCookie,
			Description: "The session cookie that signing in sets.",
		},
	}
	return doc
}
