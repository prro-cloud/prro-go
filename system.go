package prro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// SystemService — службові маршрути: версія сервісу, дозволи токена, режим
// роботи з ДПС.
type SystemService service

// Version повертає версію збірки сервісу: "dev" у локальній збірці, інакше
// тег релізу ("v0.3.0"). Маршрут не потребує токена.
func (s *SystemService) Version(ctx context.Context) (string, error) {
	out, err := call[struct {
		Version string `json:"version"`
	}](ctx, s.client, request{method: http.MethodGet, path: "/v1/version"})
	if err != nil {
		return "", err
	}
	return out.Version, nil
}

// Identity — клієнт, від імені якого діє токен, і межі його дозволів.
type Identity struct {
	// ClientID — клієнт, від імені якого діє токен.
	ClientID string `json:"client_id"`
	// Scopes — дозволи, видані токену.
	Scopes []string `json:"scopes"`
	// AllCashRegisters — токен діє на всі каси клієнта, поточні й
	// майбутні; false — лише на перелічені в CashRegisters.
	AllCashRegisters bool `json:"all_cash_registers"`
	// CashRegisters — ідентифікатори кас обмеженого токена; порожній, коли
	// AllCashRegisters.
	CashRegisters []string `json:"cash_registers,omitempty"`
	// TokenID — ідентифікатор токена (jti); за ним токен відкликають у
	// кабінеті.
	TokenID string `json:"jti"`
	// ExpiresAt — коли токен спливає; нульовий час — токен безстроковий.
	ExpiresAt time.Time `json:"expires_at,omitzero"`
}

// UnmarshalJSON приймає expires_at і як рядок RFC 3339 (за контрактом), і
// як число секунд Unix — так його віддають сервери до виправлення формату.
func (id *Identity) UnmarshalJSON(b []byte) error {
	type plain Identity
	var aux struct {
		*plain
		ExpiresAt json.RawMessage `json:"expires_at"`
	}
	aux.plain = (*plain)(id)
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	raw := string(aux.ExpiresAt)
	switch {
	case raw == "" || raw == "null":
		id.ExpiresAt = time.Time{}
	case raw[0] == '"':
		return json.Unmarshal(aux.ExpiresAt, &id.ExpiresAt)
	default:
		sec, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("prro: expires_at: %w", err)
		}
		id.ExpiresAt = time.Unix(sec, 0).UTC()
	}
	return nil
}

// WhoAmI повертає клієнта і дозволи токена — найпростіший спосіб
// перевірити, що токен дійсний.
func (s *SystemService) WhoAmI(ctx context.Context) (*Identity, error) {
	return call[Identity](ctx, s.client, request{method: http.MethodGet, path: "/v1/whoami"})
}

// DPSMode — режим роботи сервісу з ДПС.
type DPSMode string

// Режими роботи з ДПС.
const (
	// DPSOnline — ДПС відповідає вчасно.
	DPSOnline DPSMode = "online"
	// DPSDegraded — ДПС відповідає, але повільно; буває лише у
	// [DPSStatus.Verdict].
	DPSDegraded DPSMode = "degraded"
	// DPSOffline — ДПС недоступна, каси працюють в офлайн-сесіях.
	DPSOffline DPSMode = "offline"
)

// DPSStatus — режим роботи з ДПС і телеметрія, що його пояснює.
type DPSStatus struct {
	// Effective — підсумковий режим, якого дотримується сервіс:
	// [DPSOnline] або [DPSOffline].
	Effective DPSMode `json:"effective"`
	// Verdict — вердикт детектора; відрізняє «повільно» ([DPSDegraded]) від
	// «недоступно».
	Verdict DPSMode `json:"verdict"`
	// Since — відколи діє поточний режим Effective.
	Since time.Time `json:"since"`
	// LatencyMS — затримка успішних викликів у вікні детектора, мс.
	LatencyMS int64 `json:"latency_ms"`
	// Samples — скільки викликів у вікні.
	Samples int `json:"samples"`
	// Failures — скільки з них невдалих.
	Failures int `json:"failures"`
	// LastOKAt — остання успішна відповідь ДПС.
	LastOKAt time.Time `json:"last_ok_at,omitzero"`
	// LastProbeAt — остання перевірка доступності ДПС.
	LastProbeAt time.Time `json:"last_probe_at,omitzero"`
	// Tunables — пороги, за якими судять числа вище.
	Tunables DPSTunables `json:"tunables"`
}

// DPSTunables — пороги детектора доступності ДПС.
type DPSTunables struct {
	// WindowSeconds — ширина вікна детектора, с.
	WindowSeconds int64 `json:"window_seconds"`
	// DegradedLatencyMS — затримка, від якої відповідь вважають
	// повільною, мс.
	DegradedLatencyMS int64 `json:"degraded_latency_ms"`
	// ProbeIntervalSeconds — крок перевірок, с.
	ProbeIntervalSeconds int64 `json:"probe_interval_seconds"`
}

// DPS повертає поточний режим роботи сервісу з ДПС; доступний будь-якому
// машинному токену.
func (s *SystemService) DPS(ctx context.Context) (*DPSStatus, error) {
	return call[DPSStatus](ctx, s.client, request{method: http.MethodGet, path: "/v1/system/dps"})
}
