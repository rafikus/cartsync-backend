// internal/email/email.go

package email

import (
	"fmt"
	"log"
	"net/smtp"
	"os"
	"strings"
)

// Config holds email server configuration
type Config struct {
	SMTPHost     string
	SMTPPort     string
	SMTPUser     string
	SMTPPassword string
	FromAddress  string
	Enabled      bool
}

var config Config

func init() {
	config = Config{
		SMTPHost:     os.Getenv("SMTP_HOST"),
		SMTPPort:     os.Getenv("SMTP_PORT"),
		SMTPUser:     os.Getenv("SMTP_USER"),
		SMTPPassword: os.Getenv("SMTP_PASSWORD"),
		FromAddress:  os.Getenv("SMTP_FROM"),
		Enabled:      os.Getenv("SMTP_ENABLED") == "true",
	}

	if !config.Enabled {
		log.Println("email: SMTP disabled, reset tokens will be logged to console")
	} else {
		log.Printf("email: SMTP enabled, sending from %s", config.FromAddress)
	}
}

// SendPasswordReset sends a password reset email
func SendPasswordReset(toEmail, resetToken string) error {
	// For development/testing: just log the token
	if !config.Enabled {
		log.Printf("=== PASSWORD RESET TOKEN FOR %s ===", toEmail)
		log.Printf("Token: %s", resetToken)
		log.Printf("=========================================")
		return nil
	}

	subject := "Reset Your Password - CartSync"
	body := fmt.Sprintf(`
Hello,

You requested to reset your password for CartSync.

Your password reset code is: %s

This code will expire in 1 hour.

If you didn't request this, please ignore this email.

Best regards,
The CartSync Team
`, resetToken)

	return sendEmail(toEmail, subject, body)
}

func sendEmail(to, subject, body string) error {
	from := config.FromAddress
	
	msg := []string{
		fmt.Sprintf("From: %s", from),
		fmt.Sprintf("To: %s", to),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		body,
	}
	
	message := []byte(strings.Join(msg, "\r\n"))
	
	auth := smtp.PlainAuth("", config.SMTPUser, config.SMTPPassword, config.SMTPHost)
	addr := fmt.Sprintf("%s:%s", config.SMTPHost, config.SMTPPort)
	
	return smtp.SendMail(addr, auth, from, []string{to}, message)
}
