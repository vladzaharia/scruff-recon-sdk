module github.com/vladzaharia/scruff-recon-sdk/scruff

go 1.25

require (
	github.com/coder/websocket v1.8.14
	github.com/vladzaharia/scruff-recon-sdk/core v0.2.0
)

// v0.2.0 shipped against core v0.1.0, which predates core.Request.AddQuery and
// AddForm, so it does not compile for consumers. Local builds missed it because
// the replace below resolves core from the working tree.
retract v0.2.0

// Ignored by consumers -- Go applies replace directives only from the main
// module -- but it lets this repo build from a clone before core is tagged.
replace github.com/vladzaharia/scruff-recon-sdk/core => ../core
