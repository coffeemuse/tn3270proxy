package quickstart

import "encoding/json"

// configDoc mirrors the subset of internal/config's on-disk JSON shape that
// quickstart sets. Field names/tags MUST match internal/config.fileConfig; a
// round-trip test through config.Load (a later task) guards against drift.
type configDoc struct {
	DB        string `json:"db"`
	Listeners struct {
		Plain struct {
			Enabled bool   `json:"enabled"`
			Addr    string `json:"addr"`
		} `json:"plain"`
		TLS struct {
			Enabled bool   `json:"enabled"`
			Addr    string `json:"addr"`
			Cert    string `json:"cert"`
			Key     string `json:"key"`
		} `json:"tls"`
	} `json:"listeners"`
	MFA struct {
		KeyFile string `json:"key_file"`
	} `json:"mfa"`
}

// renderConfigJSON builds the quick-start tn3270proxy.json: db path, both
// listeners enabled, and the MFA key file. Returned bytes are indented.
func renderConfigJSON(l Layout) ([]byte, error) {
	var d configDoc
	d.DB = l.DB
	d.Listeners.Plain.Enabled = true
	d.Listeners.Plain.Addr = PlainAddr
	d.Listeners.TLS.Enabled = true
	d.Listeners.TLS.Addr = TLSAddr
	d.Listeners.TLS.Cert = l.Cert
	d.Listeners.TLS.Key = l.Key
	d.MFA.KeyFile = l.MFAKey
	return json.MarshalIndent(d, "", "  ")
}
