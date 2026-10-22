package prro_test

import (
	"strings"
	"testing"

	"github.com/prro-cloud/prro-go"
	"github.com/prro-cloud/prro-go/webhook"
)

// Дані подій вебхуків, для яких контракт описує схему.
func TestContractWebhookData(t *testing.T) {
	data := "ReceiptRegisteredData"
	prop := func(path ...string) []string { return append([]string{data, "properties"}, path...) }

	prro.CheckContractSchema(t, webhook.ReceiptData{}, nil, data)
	prro.CheckContractSchema(t, webhook.ReceiptLine{}, nil, prop("items", "items")...)
	prro.CheckContractSchema(t, webhook.ReceiptPayment{}, nil, prop("payments", "items")...)
	prro.CheckContractSchema(t, prro.TaxTotal{}, nil, "TaxTotal")
	prro.CheckContractSchema(t, prro.ReturnOriginal{}, nil, prop("original")...)

	prro.CheckContractEnum(t, []string{
		string(prro.ReceiptSale), string(prro.ReceiptReturn), string(prro.ReceiptStorno),
		string(prro.ReceiptServiceIn), string(prro.ReceiptServiceOut),
	}, prop("type")...)
	prro.CheckContractEnum(t, []string{string(webhook.SourceAPI), string(webhook.SourceCabinet)}, prop("source")...)
	if items := prro.ContractNode(t, prop("taxes", "items")...); !strings.Contains(items, `"#/components/schemas/TaxTotal"`) {
		t.Errorf("ReceiptRegisteredData.taxes.items = %q, want $ref TaxTotal", items)
	}

	prro.CheckContractSchema(t, webhook.ShiftData{}, nil, "ShiftEventData")
}
