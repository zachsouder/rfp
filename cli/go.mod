module github.com/zachsouder/rfp/cli

go 1.24.0

require (
	github.com/spf13/cobra v1.10.2
	github.com/zachsouder/rfp/discovery v0.0.0
	github.com/zachsouder/rfp/shared v0.0.0
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.8.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/text v0.34.0 // indirect
)

replace github.com/zachsouder/rfp/shared => ../shared

replace github.com/zachsouder/rfp/discovery => ../discovery
