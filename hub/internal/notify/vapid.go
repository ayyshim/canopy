package notify

import (
	"context"

	webpush "github.com/SherClockHolmes/webpush-go"

	"canopy/hub/internal/store"
	"canopy/hub/internal/store/db"
)

// VAPID is the key pair that identifies this hub to browser push services.
type VAPID struct{ Public, Private string }

// LoadVAPID returns the keys from the environment values when both are set, else the stored
// pair, creating it on first use.
func LoadVAPID(ctx context.Context, st *store.Store, envPublic, envPrivate string) (VAPID, error) {
	if envPublic != "" && envPrivate != "" {
		return VAPID{Public: envPublic, Private: envPrivate}, nil
	}
	row, err := st.GetVapidKeys(ctx)
	if store.IsNotFound(err) {
		priv, pub, gerr := webpush.GenerateVAPIDKeys()
		if gerr != nil {
			return VAPID{}, gerr
		}
		// A hub starting twice at once keeps whichever pair landed first.
		if _, err := st.CreateVapidKeys(ctx, db.CreateVapidKeysParams{PublicKey: pub, PrivateKey: priv}); err != nil && !store.IsNotFound(err) {
			return VAPID{}, err
		}
		row, err = st.GetVapidKeys(ctx)
	}
	if err != nil {
		return VAPID{}, err
	}
	return VAPID{Public: row.PublicKey, Private: row.PrivateKey}, nil
}
