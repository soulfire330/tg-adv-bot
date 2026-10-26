package tg

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

// Login проводит интерактивный вход и сохраняет сессию на volume.
func Login(ctx context.Context, client *telegram.Client, phone string) error {
	reader := bufio.NewReader(os.Stdin)
	return client.Run(ctx, func(ctx context.Context) error {
		flow := auth.NewFlow(
			terminalAuth{phone: phone, in: reader, out: os.Stdout},
			auth.SendCodeOptions{},
		)
		if err := client.Auth().IfNecessary(ctx, flow); err != nil {
			return fmt.Errorf("вход: %w", err)
		}
		self, err := client.Self(ctx)
		if err != nil {
			return fmt.Errorf("профиль: %w", err)
		}
		fmt.Printf("✅ авторизован: %s @%s (id %d)\n", self.FirstName, self.Username, self.ID)
		return nil
	})
}

type terminalAuth struct {
	phone string
	in    *bufio.Reader
	out   io.Writer
}

func (a terminalAuth) Phone(context.Context) (string, error) { return a.phone, nil }

func (a terminalAuth) Password(context.Context) (string, error) {
	return a.prompt("Пароль 2FA (пусто, если не задан): ")
}

func (a terminalAuth) Code(context.Context, *tg.AuthSentCode) (string, error) {
	return a.prompt("Код из Telegram: ")
}

func (a terminalAuth) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return nil
}

func (a terminalAuth) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("незарегистрированные аккаунты не поддерживаются")
}

func (a terminalAuth) prompt(question string) (string, error) {
	fmt.Fprint(a.out, question)
	line, err := a.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
