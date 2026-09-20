module github.com/vladzaharia/scruff-recon-sdk/scruff

go 1.25

require (
	github.com/coder/websocket v1.8.14
	github.com/vladzaharia/scruff-recon-sdk/core v0.1.0
)

// Ignored by consumers -- Go applies replace directives only from the main
// module -- but it lets this repo build from a clone before core is tagged.
replace github.com/vladzaharia/scruff-recon-sdk/core => ../core
