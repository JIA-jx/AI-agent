package event

import "basemodel/errs"

type Event struct {
	Name string
	Data any
}
type Handler func(e Event) (any, error)

var handlers = make(map[string]Handler)

func Register(eventName string, handler Handler) {
	handlers[eventName] = handler
}

func Trigger(eventName string, data interface{}) (any, error) {
	if handler, ok := handlers[eventName]; ok {
		return handler(Event{Name: eventName, Data: data})
	}

	return nil, errs.NoEventHandler
}
