package prro

import (
	"strings"
	"testing"
)

// Звірка з контрактом для зовнішніх тестів (package prro_test): підпакет
// webhook імпортує prro, тож внутрішній тест не може імпортувати його
// типи без циклу.

// CheckContractSchema звіряє поля схеми components.schemas.<path> з
// json-полями структури typ в обидва боки.
func CheckContractSchema(t *testing.T, typ any, skip []string, path ...string) {
	t.Helper()
	checkSchema(t, loadSpec(t).at(append([]string{"components", "schemas"}, path...)...), typ, skip)
}

// CheckContractEnum звіряє перелічення components.schemas.<path> з
// константами want.
func CheckContractEnum(t *testing.T, want []string, path ...string) {
	t.Helper()
	node := loadSpec(t).at(append([]string{"components", "schemas"}, path...)...)
	checkEnum(t, node, strings.Join(path, "."), want, nil)
}
