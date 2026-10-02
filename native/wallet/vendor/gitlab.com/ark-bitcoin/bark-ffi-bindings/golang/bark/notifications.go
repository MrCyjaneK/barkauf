package bark

import "context"

// NotificationChan returns a receive-only channel that streams WalletNotification
// events from the wallet until the context is cancelled or the notification holder
// signals it is done (NextNotification returns nil).
//
// A fresh NotificationHolder is created on each call.  The channel is closed when
// the loop ends (cancellation, nil event, or error).
//
// Usage:
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	ch, err := bark.NotificationChan(ctx, wallet)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for event := range ch {
//	    switch e := event.(type) {
//	    case WalletNotificationMovementCreated:
//	        fmt.Println("movement created:", e.Movement.Id)
//	    case WalletNotificationMovementUpdated:
//	        fmt.Println("movement updated:", e.Movement.Id)
//	    case WalletNotificationChannelLagging:
//	        fmt.Println("channel lagging")
//	    }
//	}
func NotificationChan(ctx context.Context, wallet WalletInterface) (<-chan WalletNotification, error) {
	holder := wallet.Notifications()

	out := make(chan WalletNotification)

	go func() {
		defer close(out)
		defer holder.CancelNextNotificationWait()

		// Unblock a blocked NextNotification when the context is cancelled.
		go func() {
			<-ctx.Done()
			holder.CancelNextNotificationWait()
		}()

		for {
			event, err := holder.NextNotification()
			if err != nil || event == nil {
				return
			}
			select {
			case out <- *event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}
