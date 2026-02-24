package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"clawkangsar/internal/config"
	"clawkangsar/internal/core"
)

type Gateway struct {
	bot       *bot.Bot
	logger    *slog.Logger
	processor core.Processor
	allowList map[int64]struct{}
}

func New(cfg config.TelegramConfig, processor core.Processor, logger *slog.Logger) (*Gateway, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("telegram token is required when telegram.enabled=true")
	}

	gateway := &Gateway{
		logger:    logger,
		processor: processor,
		allowList: make(map[int64]struct{}, len(cfg.AllowList)),
	}
	for _, id := range cfg.AllowList {
		gateway.allowList[id] = struct{}{}
	}

	telegramBot, err := bot.New(cfg.Token, bot.WithDefaultHandler(gateway.handleUpdate))
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}
	gateway.bot = telegramBot

	return gateway, nil
}

func (g *Gateway) Start(ctx context.Context) error {
	g.logger.Info("telegram gateway started")
	g.bot.Start(ctx)
	g.logger.Info("telegram gateway stopped")
	return nil
}

func (g *Gateway) handleUpdate(ctx context.Context, _ *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil || update.Message.From == nil {
		return
	}

	userID := update.Message.From.ID
	if !g.isAllowed(userID) {
		g.logger.Warn("telegram user rejected by allow list", "user_id", userID)
		_, _ = g.bot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "Unauthorized.",
		})
		return
	}

	text := strings.TrimSpace(update.Message.Text)
	if text == "" {
		return
	}

	reply, err := g.processor.Process(ctx, core.Message{
		Channel:   "telegram",
		UserID:    strconv.FormatInt(userID, 10),
		ChatID:    strconv.FormatInt(update.Message.Chat.ID, 10),
		Text:      text,
		Timestamp: time.Now(),
	})
	if err != nil {
		g.logger.Error("telegram processing error", "error", err, "user_id", userID)
		reply = "Request failed."
	}
	if strings.TrimSpace(reply) == "" {
		return
	}

	if _, err := g.bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   reply,
	}); err != nil {
		g.logger.Error("telegram send error", "error", err, "user_id", userID)
	}
}

func (g *Gateway) isAllowed(userID int64) bool {
	if len(g.allowList) == 0 {
		return false
	}
	_, ok := g.allowList[userID]
	return ok
}
