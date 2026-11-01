package websocket

import "testing"

func TestRoomSyncStrictStructure(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct{ body, code string }{
		{`{"type":"room.sync","request_id":"` + id + `","payload":{}}`, ""},
		{`{"type":"room.sync","payload":{}}`, "VALIDATION_ERROR"},
		{`{"type":"room.sync","request_id":"` + id + `","payload":{"room_id":"other"}}`, "VALIDATION_ERROR"},
		{`{"type":"room.sync","request_id":"` + id + `","payload":null}`, "VALIDATION_ERROR"},
	} {
		c, f := decode([]byte(tc.body))
		if f.Code != tc.code {
			t.Fatalf("%s: got %+v, %+v", tc.body, c, f)
		}
	}
	c, f := decode([]byte(`{"type":"room.sync","request_id":"` + id + `","payload":{"extra":1}}`))
	if f.Code != "VALIDATION_ERROR" || rejectedCommand(c).RequestID != id {
		t.Fatal("invalid sync bypassed ordinary dedup")
	}
}
