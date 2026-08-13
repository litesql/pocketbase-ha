package feature

import "github.com/pocketbase/pocketbase/core"

// Feature is an optional capability registered on the PocketBase app.
// Register may bind OnServe, filesystem hooks, or other PocketBase events.
type Feature interface {
	Name() string
	Register(app core.App) error
}
