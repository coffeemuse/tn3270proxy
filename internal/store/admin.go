package store

// Reserved group namespace. Group names with the (case-insensitive) prefix
// ReservedGroupPrefix are dictated by the app: the admin UI can manage their
// membership but can never create or delete them. AdminGroup is the first such
// group; membership in it gates the 3270 admin screens. It is auto-created by
// migrate() so it always exists.
const (
	AdminGroup          = "ZZADMIN"
	ReservedGroupPrefix = "ZZ"
)
