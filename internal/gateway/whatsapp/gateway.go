package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"clawkangsar/internal/config"
	"clawkangsar/internal/core"
)

type Gateway struct {
	client    *whatsmeow.Client
	logger    *slog.Logger
	processor core.Processor
	qrCancel  context.CancelFunc
}

func New(cfg config.WhatsAppConfig, processor core.Processor, logger *slog.Logger) (*Gateway, error) {
	if logger == nil {
		logger = slog.Default()
	}

	container, err := sqlstore.New(context.Background(), "sqlite3", cfg.SessionDSN, waLog.Stdout("WhatsAppDB", "WARN", false))
	if err != nil {
		return nil, fmt.Errorf("create whatsapp sql store: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("get whatsapp device store: %w", err)
	}

	client := whatsmeow.NewClient(deviceStore, waLog.Stdout("WhatsApp", "WARN", false))
	gateway := &Gateway{
		client:    client,
		logger:    logger,
		processor: processor,
	}
	client.AddEventHandler(gateway.handleEvent)

	return gateway, nil
}

func (g *Gateway) Start(ctx context.Context) error {
	if g.client.Store.ID == nil {
		qrCtx, cancel := context.WithCancel(ctx)
		g.qrCancel = cancel

		qrChannel, err := g.client.GetQRChannel(qrCtx)
		if err != nil {
			return fmt.Errorf("init whatsapp qr channel: %w", err)
		}
		go g.consumeQR(qrChannel)
		g.logger.Info("whatsapp requires pairing; QR printed to terminal")
	} else {
		g.logger.Info("whatsapp restored previous session", "jid", g.client.Store.ID.String())
	}

	if err := g.client.Connect(); err != nil {
		return fmt.Errorf("connect whatsapp: %w", err)
	}
	g.logger.Info("whatsapp gateway started")

	<-ctx.Done()
	if g.qrCancel != nil {
		g.qrCancel()
	}
	g.client.Disconnect()
	g.logger.Info("whatsapp gateway stopped")
	return nil
}

func (g *Gateway) consumeQR(qrChannel <-chan whatsmeow.QRChannelItem) {
	for evt := range qrChannel {
		switch evt.Event {
		case "code":
			fmt.Fprintln(os.Stdout, "Scan this WhatsApp QR code from Linked Devices:")
			qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
		default:
			g.logger.Info("whatsapp qr event", "event", evt.Event)
		}
	}
}

func (g *Gateway) handleEvent(rawEvent interface{}) {
	event, ok := rawEvent.(*events.Message)
	if !ok {
		return
	}
	if event.Info.IsFromMe {
		return
	}

	text := extractText(event.Message)
	if text == "" {
		return
	}

	reply, err := g.processor.Process(context.Background(), core.Message{
		Channel:   "whatsapp",
		UserID:    event.Info.Sender.String(),
		ChatID:    event.Info.Chat.String(),
		Text:      text,
		Timestamp: event.Info.Timestamp,
	})
	if err != nil {
		g.logger.Error("whatsapp processing error", "error", err, "chat", event.Info.Chat.String())
		reply = "Request failed."
	}

	reply = strings.TrimSpace(reply)
	if reply == "" {
		return
	}

	if _, err := g.client.SendMessage(context.Background(), event.Info.Chat, &waProto.Message{
		Conversation: proto.String(reply),
	}); err != nil {
		g.logger.Error("whatsapp send error", "error", err, "chat", event.Info.Chat.String())
	}
}

func extractText(message *waProto.Message) string {
	if message == nil {
		return ""
	}
	if text := strings.TrimSpace(message.GetConversation()); text != "" {
		return text
	}
	if ext := message.GetExtendedTextMessage(); ext != nil {
		if text := strings.TrimSpace(ext.GetText()); text != "" {
			return text
		}
	}
	return ""
}
