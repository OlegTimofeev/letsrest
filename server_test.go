package letsrest

import (
	"github.com/gavv/httpexpect"
	"net/http"
	"testing"
)

var store = NewRequestStore()

type testRequester struct {
}

func (r *testRequester) Do(request *RequestTask) (*Response, error) {
	return nil, nil
}

func TestServer_SimpleApiCalls(t *testing.T) {
	tester(t).GET("/").
		Expect().
		Status(http.StatusOK)

	tester(t).GET("/api/v1").
		Expect().
		Status(http.StatusOK)
}

func TestServer_CreateRequest(t *testing.T) {
	cReq := createRequest(t)

	getResp := tester(t).GET("/api/v1/requests/{reqID}", cReq.ID).
		Expect().
		Status(http.StatusOK).
		JSON()

	getResp.Object().Equal(cReq)
	getResp.Object().ValueEqual("id", cReq.ID)
}

func TestServer_GetNotExistedRequest(t *testing.T) {
	v := tester(t).GET("/api/v1/requests/{reqID}", "someNotExistedID").
		Expect().
		Status(http.StatusNotFound).
		JSON()

	v.Object().Value("key").Equal(ReqNotFoundKey)
	v.Object().ValueEqual("params", Params{"id": "someNotExistedID"})
}

func TestServer_GetReadyResponse(t *testing.T) {
	cReq := createRequest(t)

	resp := &Response{ID: cReq.ID, StatusCode: 200}

	store.SetResponse(cReq.ID, resp)

	obj := tester(t).GET("/api/v1/requests/{reqID}/responses", cReq.ID).
		Expect().
		Status(http.StatusOK).
		JSON().Object()

	obj.ValueEqual("response", resp)
	obj.Value("info").Object().ValueEqual("status", "done")
}

func TestServer_GetNotReadyResponse(t *testing.T) {
	cReq := createRequest(t)

	r := tester(t).GET("/api/v1/requests/{reqID}/responses", cReq.ID).
		Expect().
		Status(http.StatusPartialContent).
		JSON().Object()

	r.Value("info").Object().ValueEqual("status", "in_progress")
}

func createRequest(t *testing.T) *RequestTask {
	cReq := &RequestTask{URL: "http://somedomain.com", Method: "POST"}

	resp := tester(t).PUT("/api/v1/requests").
		WithJSON(cReq).
		Expect().
		Status(http.StatusCreated).
		JSON()

	resp.Object().ContainsKey("id")
	cReq.ID = resp.Object().Value("id").Raw().(string)
	resp.Object().Equal(cReq)

	return cReq
}

func tester(t *testing.T) *httpexpect.Expect {
	return IrisHandler(&testRequester{}, store).Tester(t)
}
