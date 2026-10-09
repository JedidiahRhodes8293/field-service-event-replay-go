package fieldservice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

type ReplayService struct {
	infrai *Client
	store  *WorkOrderStore
	secret string
}

type WorkOrderStore struct {
	orders map[string]WorkOrderState
}

type WorkOrderState struct {
	DispatchStatus   string `json:"dispatch_status"`
	PhotoCount       int    `json:"photo_count"`
	FollowUpRequired bool   `json:"follow_up_required"`
	FollowUpNote     string `json:"follow_up_note,omitempty"`
}

type ReplayResult struct {
	Replayed   int                        `json:"replayed"`
	WorkOrders map[string]WorkOrderState  `json:"work_orders"`
}

type FieldEvent struct {
	Type      string          `json:"type"`
	WorkOrder string          `json:"work_order_id"`
	Payload   json.RawMessage `json:"payload"`
}

type PhotoUploaded struct {
	PhotoID string `json:"photo_id"`
}

type DispatchChanged struct {
	Status string `json:"status"`
}

type FollowUpCreated struct {
	Note string `json:"note"`
}

type FollowUpCompleted struct{}

func NewReplayService(infrai *Client, secret string) *ReplayService {
	return &ReplayService{
		infrai: infrai,
		store: &WorkOrderStore{
			orders: map[string]WorkOrderState{},
		},
		secret: secret,
	}
}

func (s *ReplayService) ReplayFromWebhookDeliveries(ctx context.Context, webhookID string) (ReplayResult, error) {
	deliveries, err := s.infrai.AccountWebhookDeliveries(ctx, webhookID)
	if err != nil {
		return ReplayResult{}, err
	}
	for _, d := range deliveries.Deliveries {
		if err := s.ApplyRawEvent([]byte(d.RequestBody)); err != nil {
			return ReplayResult{}, err
		}
	}
	return s.store.Snapshot(len(deliveries.Deliveries)), nil
}

func (s *ReplayService) ApplySignedEvent(body []byte, signatureHex string) error {
	if !VerifySignature(body, s.secret, signatureHex) {
		return errors.New("invalid signature")
	}
	return s.ApplyRawEvent(body)
}

func (s *ReplayService) ApplyRawEvent(body []byte) error {
	var event FieldEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return err
	}
	s.store.Apply(event)
	return nil
}

func (s *ReplayService) RedriveDeadLetters(ctx context.Context, queue string) error {
	_, err := s.infrai.QueueDLQRedrive(ctx, queue)
	return err
}

func (ws *WorkOrderStore) Apply(event FieldEvent) {
	state := ws.orders[event.WorkOrder]
	switch event.Type {
	case "work_order.photo_uploaded":
		state.PhotoCount++
	case "dispatch.status_changed":
		var payload DispatchChanged
		_ = json.Unmarshal(event.Payload, &payload)
		state.DispatchStatus = payload.Status
	case "technician.follow_up_created":
		var payload FollowUpCreated
		_ = json.Unmarshal(event.Payload, &payload)
		state.FollowUpRequired = true
		state.FollowUpNote = payload.Note
	case "technician.follow_up_completed":
		state.FollowUpRequired = false
	}
	ws.orders[event.WorkOrder] = state
}

func (ws *WorkOrderStore) Snapshot(replayed int) ReplayResult {
	out := map[string]WorkOrderState{}
	keys := make([]string, 0, len(ws.orders))
	for id := range ws.orders {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		out[id] = ws.orders[id]
	}
	return ReplayResult{Replayed: replayed, WorkOrders: out}
}

func ComputeSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifySignature(body []byte, secret, got string) bool {
	expected := ComputeSignature(body, secret)
	return hmac.Equal([]byte(expected), []byte(got))
}
