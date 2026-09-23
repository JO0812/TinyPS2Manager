// Package api exposes the REST + SSE surface (spec §6.5) served on localhost
// inside the Wails app shell. Handlers are thin and delegate to internal/queue
// and internal/library. Unknown fields are rejected at input; enqueue goes
// through oplfs.ValidatePlan before inserting jobs (non-functional invariant N4).
//
// SSE (not WebSocket) is used for live progress to match the Wails embedding
// model and simplify the client.
package api
