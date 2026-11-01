package websocket

import "testing"

func TestDecodeCommands(t *testing.T) {
	const id = "12345678-1234-4123-8123-123456789abc"
	for _, tc := range []struct{ body, code string }{
		{`{"type":"world.ready","request_id":"` + id + `","payload":{"world_version":"v1","movement_epoch":1}}`, ""},
		{`{"type":"player.move","payload":{"movement_epoch":1,"seq":1,"position":{"x":0,"y":0,"z":0},"rotation":{"yaw":0,"pitch":0}}}`, ""},
		{`{"type":"world.ready","request_id":"` + id + `","payload":{"world_version":"v1","movement_epoch":1.00000000000000001}}`, "VALIDATION_ERROR"},
		{`{"type":"world.ready","request_id":"` + id + `","payload":{"world_version":"v1","movement_epoch":9007199254740991.1}}`, "VALIDATION_ERROR"},
		{`{`, "INVALID_JSON"}, {`null`, "VALIDATION_ERROR"}, {`{"type":"move","payload":{}}`, "UNKNOWN_EVENT"},
		{`{"type":"world.ready","payload":{}}`, "VALIDATION_ERROR"},
		{`{"type":"chat.send","request_id":"bad","payload":{"text":"hi"}}`, "VALIDATION_ERROR"},
		{`{"type":"chat.send","request_id":"` + id + `","payload":{"text":null}}`, "VALIDATION_ERROR"},
		{`{"type":"chat.send","request_id":"` + id + `","payload":{"text":"  "}}`, "VALIDATION_ERROR"},
		{`{"type":"chat.send","request_id":"` + id + `","payload":{"text":"hi","extra":1}}`, "VALIDATION_ERROR"},
		{`{"type":"world.ready","request_id":"` + id + `","payload":{"world_version":"v1","movement_epoch":9007199254740992}}`, "VALIDATION_ERROR"},
		{`{"type":"guide.ask","request_id":"` + id + `","payload":{"guide_id":"g","text":"hi","exhibit_id":null}}`, "VALIDATION_ERROR"},
		{`{"type":"world.ready","type":"world.ready","request_id":"` + id + `","payload":{"world_version":"v1","movement_epoch":1}}`, "VALIDATION_ERROR"},
		{`{"type":"chat.send","request_id":"` + id + `","payload":{"text":"\ud800"}}`, "INVALID_JSON"},
	} {
		t.Run(tc.body, func(t *testing.T) {
			_, f := decode([]byte(tc.body))
			if f.Code != tc.code {
				t.Fatalf("got %s want %s", f.Code, tc.code)
			}
		})
	}
}
