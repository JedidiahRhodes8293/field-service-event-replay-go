package fieldservice

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkOrderProjectionFromEvents(t *testing.T) {
	service := NewReplayService(&Client{}, "topsecret")

	events := []FieldEvent{
		{Type: "work_order.photo_uploaded", WorkOrder: "WO-77", Payload: mustJSON(t, PhotoUploaded{PhotoID: "P-1"})},
		{Type: "dispatch.status_changed", WorkOrder: "WO-77", Payload: mustJSON(t, DispatchChanged{Status: "arrived"})},
		{Type: "technician.follow_up_created", WorkOrder: "WO-77", Payload: mustJSON(t, FollowUpCreated{Note: "call tenant after valve photo review"})},
		{Type: "technician.follow_up_completed", WorkOrder: "WO-77", Payload: mustJSON(t, FollowUpCompleted{})},
	}

	for _, tc := range events {
		body, _ := json.Marshal(tc)
		if err := service.ApplyRawEvent(body); err != nil {
			t.Fatal(err)
		}
	}

	result := service.store.Snapshot(len(events))
	got := result.WorkOrders["WO-77"]
	if got.DispatchStatus != "arrived" {
		t.Fatalf("dispatch status = %q", got.DispatchStatus)
	}
	if got.FollowUpRequired {
		t.Fatalf("follow_up_required = true, want false")
	}
	if got.PhotoCount != 1 {
		t.Fatalf("photo_count = %d, want 1", got.PhotoCount)
	}
}

func TestWebhookSignatureVerification(t *testing.T) {
	service := NewReplayService(&Client{}, "topsecret")
	h := NewHTTPHandler(service, "wh_123", "fieldservice-followups-dlq")

	body := []byte(`{"type":"dispatch.status_changed","work_order_id":"WO-9","payload":{"status":"en_route"}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/fieldservice", bytes.NewReader(body))
	req.Header.Set("X-FieldService-Signature", ComputeSignature(body, "topsecret"))
	res := httptest.NewRecorder()

	h.Routes().ServeHTTP(res, req)

	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusAccepted)
	}
	state := service.store.Snapshot(1).WorkOrders["WO-9"]
	if state.DispatchStatus != "en_route" {
		t.Fatalf("dispatch status = %q", state.DispatchStatus)
	}
}

func TestReplayReadsEnvelopeBeforeStatus(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/v1/account/webhooks/deliveries/wh_123" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"data":{"deliveries":[{"request_body":"{\"type\":\"dispatch.status_changed\",\"work_order_id\":\"WO-55\",\"payload\":{\"status\":\"completed\"}}"}]},"metadata":{}}`))
	}))
	defer api.Close()

	client := &Client{baseURL: api.URL, apiKey: "test", httpClient: api.Client()}
	service := NewReplayService(client, "topsecret")

	result, err := service.ReplayFromWebhookDeliveries(context.Background(), "wh_123")
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed != 1 {
		t.Fatalf("replayed = %d, want 1", result.Replayed)
	}
	if result.WorkOrders["WO-55"].DispatchStatus != "completed" {
		t.Fatalf("dispatch status = %q", result.WorkOrders["WO-55"].DispatchStatus)
	}
}

func TestQueueDLQRedriveSendsQueueInBody(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/v1/queue/dlq/redrive/fieldservice-followups-dlq" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["queue"] != "fieldservice-followups-dlq" {
			t.Fatalf("queue = %v", body["queue"])
		}
		if _, ok := body["idempotency_key"]; !ok {
			t.Fatal("idempotency_key is missing")
		}
		if len(body) != 2 {
			t.Fatalf("unexpected request fields: %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"accepted":true},"metadata":{}}`))
	}))
	defer api.Close()

	client := &Client{baseURL: api.URL, apiKey: "test", httpClient: api.Client()}
	result, err := client.QueueDLQRedrive(context.Background(), "fieldservice-followups-dlq")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted {
		t.Fatal("accepted = false")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
