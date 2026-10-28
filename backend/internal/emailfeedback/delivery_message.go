package emailfeedback

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/fidibeki/app.fidibeki/backend/internal/email"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
)

type invitationMessageOptions struct {
	From             string
	PublicBaseURL    string
	WebPublicBaseURL string
	BaseDomain       string
}

type invitationMessageCopy struct {
	Heading           string
	RatingPrompt      string
	RatingScale       string
	Action            string
	Privacy           string
	OptOut            string
	DeliveryNotice    string
	PreheaderFallback string
}

func composeInvitationMessage(
	delivery InvitationDelivery,
	rawToken string,
	options invitationMessageOptions,
) (email.Message, error) {
	if delivery.RecipientEmail == nil {
		return email.Message{}, errors.New("email feedback recipient is unavailable")
	}
	formURL, err := tenant.PublicURL(
		options.WebPublicBaseURL,
		options.BaseDomain,
		delivery.TenantSlug,
		"/email-feedback/"+rawToken,
	)
	if err != nil {
		return email.Message{}, fmt.Errorf("build email feedback form URL: %w", err)
	}
	optOutURL, err := tenant.PublicURL(
		options.WebPublicBaseURL,
		options.BaseDomain,
		delivery.TenantSlug,
		"/email-feedback/"+rawToken+"/opt-out",
	)
	if err != nil {
		return email.Message{}, fmt.Errorf("build email feedback opt-out URL: %w", err)
	}
	logoURL, err := email.BrandLogoURL(options.PublicBaseURL)
	if err != nil {
		return email.Message{}, fmt.Errorf("build email feedback logo URL: %w", err)
	}
	copy := invitationCopy(delivery.Locale)
	ratingLinks := make([]email.Link, 0, 5)
	for rating := 1; rating <= 5; rating++ {
		ratingURL := *formURL
		query := url.Values{}
		query.Set("rating", strconv.Itoa(rating))
		ratingURL.RawQuery = query.Encode()
		ratingLinks = append(ratingLinks, email.Link{
			Label: strconv.Itoa(rating),
			URL:   ratingURL.String(),
		})
	}
	introduction := strings.TrimSpace(delivery.CampaignSnapshot.Introduction)
	if introduction == "" {
		introduction = copy.PreheaderFallback
	}
	return email.Compose(email.Message{
		From:           options.From,
		To:             *delivery.RecipientEmail,
		Subject:        strings.TrimSpace(delivery.CampaignSnapshot.Subject),
		IdempotencyKey: "email-feedback-invitation:" + delivery.InvitationID,
	}, email.Content{
		Preheader: introduction,
		Heading:   copy.Heading,
		Workspace: delivery.ControllerProfile.Name,
		Body: []string{
			introduction,
			copy.RatingPrompt,
			copy.RatingScale,
		},
		Choices:       ratingLinks,
		Action:        &email.Link{Label: copy.Action, URL: formURL.String()},
		ShowActionURL: true,
		Footer:        copy.DeliveryNotice,
		FooterLinks: []email.Link{
			{Label: copy.Privacy, URL: delivery.ControllerProfile.PrivacyPolicyURL},
			{Label: copy.OptOut, URL: optOutURL.String()},
		},
		LogoURL: logoURL,
	})
}

func invitationCopy(locale string) invitationMessageCopy {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "pt") {
		return invitationMessageCopy{
			Heading:           "Como foi a sua experiência?",
			RatingPrompt:      "Escolha uma classificação ou abra o formulário para partilhar mais detalhes.",
			RatingScale:       "1 significa muito má e 5 significa excelente.",
			Action:            "Partilhar feedback",
			Privacy:           "Política de privacidade",
			OptOut:            "Não receber mais estes pedidos",
			DeliveryNotice:    "Este pedido foi enviado pela organização indicada acima através da Fidibeki.",
			PreheaderFallback: "Partilhe a sua experiência.",
		}
	}
	return invitationMessageCopy{
		Heading:           "How was your experience?",
		RatingPrompt:      "Choose a rating or open the form to share more detail.",
		RatingScale:       "1 means very poor and 5 means excellent.",
		Action:            "Share feedback",
		Privacy:           "Privacy policy",
		OptOut:            "Stop receiving these requests",
		DeliveryNotice:    "This request was sent by the organization named above through Fidibeki.",
		PreheaderFallback: "Share your experience.",
	}
}
