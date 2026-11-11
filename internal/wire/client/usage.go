package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// UsageQuery asks the daemon for usage aggregates (the UsageQuery intent) and
// returns its UsageResult. A daemon that predates the intent answers
// bad_intent "intent not implemented", which the returned *Error renders as
// "run 'rex daemon restart'"; a daemon with usage off answers "usage is off".
func (c *Client) UsageQuery(ctx context.Context, q protocol.UsageQuery) (protocol.UsageReport, error) {
	env, err := c.Do(ctx, protocol.IntentUsageQuery, q)
	if err != nil {
		return protocol.UsageReport{}, err
	}
	if env.Type != protocol.EventUsageResult {
		return protocol.UsageReport{}, fmt.Errorf("%s: unexpected reply %q", protocol.IntentUsageQuery, env.Type)
	}
	var rep protocol.UsageReport
	if err := json.Unmarshal(env.Data, &rep); err != nil {
		return protocol.UsageReport{}, fmt.Errorf("%s: decode reply: %w", protocol.IntentUsageQuery, err)
	}
	return rep, nil
}
