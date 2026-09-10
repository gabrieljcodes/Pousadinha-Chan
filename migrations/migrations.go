package migrations

import _ "embed"

//go:embed 004_gacha.sql
var Gacha string

//go:embed 005_gacha_import.sql
var GachaImport string

//go:embed 006_gacha_import_payload.sql
var GachaImportPayload string
