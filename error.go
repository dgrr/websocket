package websocket

import "fmt"

type Error struct {
	Status StatusCode
	Reason string
}

func (e Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Status, e.Reason)
}
