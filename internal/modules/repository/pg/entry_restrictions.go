package pg

import (
	"context"
	"fmt"
)

// Independent of settings JSON so concurrent UI/guard saves cannot erase a ban.
func (u *User) EntryBlocked(ctx context.Context, userID int64, instID string) (bool, error) {
	var blocked bool
	err := u.db.Conn().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.instrument_entry_blocks WHERE user_id=$1 AND inst_id=$2)`, userID, instID).Scan(&blocked)
	return blocked, err
}

func (u *User) BlockEntry(ctx context.Context, userID int64, instID, code string) (bool, error) {
	if userID == 0 || instID == "" || code != "51155" {
		return false, fmt.Errorf("invalid instrument restriction")
	}
	tag, err := u.db.Conn().Exec(ctx, `INSERT INTO public.instrument_entry_blocks(user_id,inst_id,code) VALUES($1,$2,$3) ON CONFLICT (user_id,inst_id) DO NOTHING`, userID, instID, code)
	return err == nil && tag.RowsAffected() == 1, err
}
