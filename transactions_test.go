package vibecards

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListCardTransactionsScopedAndExact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if r.URL.Path != "/cards.v1.TransactionService/ListTransactions" || in["cardId"] != "card" || in["page"] != float64(2) || in["dateRange"] == nil {
			t.Errorf("unscoped request: %v", in)
		}
		_, _ = w.Write([]byte(`{"transactions":[{"id":"tx","cardId":"card","amount":"2.00","currency":"CURRENCY_USD","fee":"0.12"}],"pageInfo":{"totalPages":2}}`))
	}))
	defer server.Close()
	c := New(server.URL, "key")
	now := time.Now()
	out, err := c.ListCardTransactions(context.Background(), "card", now.Add(-24*time.Hour), now, 2)
	if err != nil || out.Transactions[0].Amount != "2.00" || *out.Transactions[0].Fee != "0.12" {
		t.Fatalf("wrong values: %+v, %v", out, err)
	}
	if _, err = c.ListCardTransactions(context.Background(), "", now.Add(-time.Hour), now, 1); err == nil {
		t.Fatal("unbounded card accepted")
	}
	var disabled *Client
	if _, err = disabled.ListCardTransactions(context.Background(), "", now, now, 1); err != ErrNotConfigured {
		t.Fatal(err)
	}
}
