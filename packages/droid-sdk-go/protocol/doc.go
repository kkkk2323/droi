// Package protocol holds the Droid Daemon's wire types, generated from the
// zod schemas of @factory/droid-sdk (see gen/). zz_types.go has every
// message, params and result shape; zz_methods.go the method and
// notification names, how each method is answered, and NewParams/NewResult
// for tools that decode frames generically.
//
// Discriminated unions (session notifications, message content blocks, ...)
// are structs with the discriminator and the raw JSON; Value decodes the
// shape. Unknown shapes decode to nil, so a newer Daemon does not break an
// older Client.
package protocol

//go:generate sh ../gen/generate.sh
