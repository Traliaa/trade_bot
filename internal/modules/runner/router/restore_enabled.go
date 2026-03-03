package router

import (
	"context"
	"log"
)

func (r *Router) RestoreEnabled(ctx context.Context) {
	// тут у тебя логика загрузки пользователей, у которых бот был включён
	users, err := r.Repository.ListEnabled(ctx) // пример
	if err != nil {
		log.Printf("RestoreEnabled: %v", err)
		return
	}

	for _, u := range users {
		u := u
		r.EnableUser(ctx, u) // ✅ repo прокинут
	}
}
