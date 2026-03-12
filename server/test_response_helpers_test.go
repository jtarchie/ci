package server_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func mustHTMLDocument(t *testing.T, rec *httptest.ResponseRecorder) *goquery.Document {
	t.Helper()

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}

	return doc
}

func mustJSONMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json response: %v", err)
	}

	return payload
}

func mustSSEJSONEvents(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()

	lines := strings.Split(rec.Body.String(), "\n")
	events := make([]map[string]any, 0)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		jsonPayload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if jsonPayload == "" {
			continue
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(jsonPayload), &event); err != nil {
			t.Fatalf("decode sse data payload: %v", err)
		}
		events = append(events, event)
	}

	if len(events) == 0 {
		t.Fatalf("no JSON SSE events found in response")
	}

	return events
}

func mustJSONErrorText(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	payload := mustJSONMap(t, rec)
	for _, key := range []string{"message", "error"} {
		value, ok := payload[key].(string)
		if ok {
			return value
		}
	}

	t.Fatalf("json error payload missing string message/error fields: %+v", payload)
	return ""
}

func hasSelectorWithText(doc *goquery.Document, selector string, text string) bool {
	return doc.Find(selector).FilterFunction(func(_ int, selection *goquery.Selection) bool {
		return strings.Contains(selection.Text(), text)
	}).Length() > 0
}

func selectorHasAttrValue(doc *goquery.Document, selector string, attr string, value string) bool {
	return doc.Find(selector).FilterFunction(func(_ int, selection *goquery.Selection) bool {
		actual, ok := selection.Attr(attr)
		return ok && actual == value
	}).Length() > 0
}

func selectorHasAttrContaining(doc *goquery.Document, selector string, attr string, value string) bool {
	return doc.Find(selector).FilterFunction(func(_ int, selection *goquery.Selection) bool {
		actual, ok := selection.Attr(attr)
		return ok && strings.Contains(actual, value)
	}).Length() > 0
}
