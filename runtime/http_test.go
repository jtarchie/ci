package runtime_test

import (
	"testing"

	"github.com/dop251/goja"
	"github.com/jtarchie/ci/runtime"
	. "github.com/onsi/gomega"
)

func TestHTTPRuntime(t *testing.T) {
	t.Parallel()

	t.Run("Request returns webhook data when present", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		jsVM := goja.New()
		jsVM.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

		webhookData := &runtime.WebhookData{
			Method:  "POST",
			URL:     "/api/webhooks/test-id?foo=bar",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    `{"key": "value"}`,
			Query:   map[string]string{"foo": "bar"},
		}

		responseChan := make(chan *runtime.HTTPResponse, 1)
		httpRuntime := runtime.NewHTTPRuntime(jsVM, webhookData, responseChan)

		err := jsVM.Set("http", httpRuntime)
		assert.Expect(err).NotTo(HaveOccurred())

		val, err := jsVM.RunString(`
			var req = http.request();
			JSON.stringify({
				method: req.method,
				url: req.url,
				body: req.body,
				headers: req.headers,
				query: req.query,
			});
		`)
		assert.Expect(err).NotTo(HaveOccurred())

		result := val.String()
		assert.Expect(result).To(ContainSubstring(`"method":"POST"`))
		assert.Expect(result).To(ContainSubstring(`"body":"{\"key\": \"value\"}"`))
		assert.Expect(result).To(ContainSubstring(`"foo":"bar"`))
	})

	t.Run("Request returns undefined when no webhook data", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		jsVM := goja.New()
		jsVM.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
		httpRuntime := runtime.NewHTTPRuntime(jsVM, nil, nil)

		err := jsVM.Set("http", httpRuntime)
		assert.Expect(err).NotTo(HaveOccurred())

		val, err := jsVM.RunString(`http.request() === undefined`)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(val.ToBoolean()).To(BeTrue())
	})

	t.Run("Respond sends response on channel", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		jsVM := goja.New()
		jsVM.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

		webhookData := &runtime.WebhookData{Method: "POST"}
		responseChan := make(chan *runtime.HTTPResponse, 1)
		httpRuntime := runtime.NewHTTPRuntime(jsVM, webhookData, responseChan)

		err := jsVM.Set("http", httpRuntime)
		assert.Expect(err).NotTo(HaveOccurred())

		_, err = jsVM.RunString(`
			http.respond({
				status: 201,
				body: "created",
				headers: { "X-Custom": "test-value" }
			});
		`)
		assert.Expect(err).NotTo(HaveOccurred())

		assert.Expect(responseChan).To(HaveLen(1))
		resp := <-responseChan
		assert.Expect(resp.Status).To(Equal(201))
		assert.Expect(resp.Body).To(Equal("created"))
		assert.Expect(resp.Headers["X-Custom"]).To(Equal("test-value"))
	})

	t.Run("Respond defaults status to 200", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		jsVM := goja.New()
		jsVM.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

		webhookData := &runtime.WebhookData{Method: "POST"}
		responseChan := make(chan *runtime.HTTPResponse, 1)
		httpRuntime := runtime.NewHTTPRuntime(jsVM, webhookData, responseChan)

		err := jsVM.Set("http", httpRuntime)
		assert.Expect(err).NotTo(HaveOccurred())

		_, err = jsVM.RunString(`http.respond({ body: "ok" })`)
		assert.Expect(err).NotTo(HaveOccurred())

		resp := <-responseChan
		assert.Expect(resp.Status).To(Equal(200))
		assert.Expect(resp.Body).To(Equal("ok"))
	})

	t.Run("Respond is one-shot - second call is ignored", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		jsVM := goja.New()
		jsVM.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

		webhookData := &runtime.WebhookData{Method: "POST"}
		responseChan := make(chan *runtime.HTTPResponse, 1)
		httpRuntime := runtime.NewHTTPRuntime(jsVM, webhookData, responseChan)

		err := jsVM.Set("http", httpRuntime)
		assert.Expect(err).NotTo(HaveOccurred())

		_, err = jsVM.RunString(`
			http.respond({ status: 200, body: "first" });
			http.respond({ status: 201, body: "second" });
		`)
		assert.Expect(err).NotTo(HaveOccurred())

		assert.Expect(responseChan).To(HaveLen(1))
		resp := <-responseChan
		assert.Expect(resp.Body).To(Equal("first"))
	})

	t.Run("Respond is no-op when no response channel", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		jsVM := goja.New()
		jsVM.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

		httpRuntime := runtime.NewHTTPRuntime(jsVM, nil, nil)

		err := jsVM.Set("http", httpRuntime)
		assert.Expect(err).NotTo(HaveOccurred())

		// Should not panic or error
		_, err = jsVM.RunString(`http.respond({ status: 200, body: "noop" })`)
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("Respond with undefined argument is no-op", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		jsVM := goja.New()
		jsVM.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

		webhookData := &runtime.WebhookData{Method: "POST"}
		responseChan := make(chan *runtime.HTTPResponse, 1)
		httpRuntime := runtime.NewHTTPRuntime(jsVM, webhookData, responseChan)

		err := jsVM.Set("http", httpRuntime)
		assert.Expect(err).NotTo(HaveOccurred())

		_, err = jsVM.RunString(`http.respond(undefined)`)
		assert.Expect(err).NotTo(HaveOccurred())

		assert.Expect(responseChan).To(HaveLen(0))
	})
}
