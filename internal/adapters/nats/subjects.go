package broker

import "fmt"

// NATS subject constants and builders.
const (
	SubjectJobCreated  = "job.created"
	SubjectJobUpdated  = "job.updated"
	SubjectURLDiscovered = "url.discovered"
	SubjectURLBlocked  = "url.blocked"
	SubjectCaptchaDetected = "captcha.detected"
	SubjectCaptchaSolved   = "captcha.solved"
	SubjectWorkerHeartbeat = "worker.heartbeat"
)

// CrawlDispatchSubject returns the subject for dispatching URLs to workers.
func CrawlDispatchSubject(jobID string) string {
	return fmt.Sprintf("crawl.dispatch.%s", jobID)
}

// CrawlResultSubject returns the subject for workers reporting results.
func CrawlResultSubject(jobID string) string {
	return fmt.Sprintf("crawl.result.%s", jobID)
}

// MetricsSubject returns the subject for job metrics.
func MetricsSubject(jobID string) string {
	return fmt.Sprintf("metrics.%s", jobID)
}

// GUIPushSubject returns the subject for GUI push events.
func GUIPushSubject(jobID string) string {
	return fmt.Sprintf("gui.push.%s", jobID)
}

// JobCommandSubject returns the subject for job commands (pause/stop/resume).
func JobCommandSubject(jobID string) string {
	return fmt.Sprintf("job.command.%s", jobID)
}

// WebhookSubject returns the subject for webhook events.
func WebhookSubject(event string) string {
	return fmt.Sprintf("webhook.%s", event)
}

// QueueGroupCrawler is the queue group name for crawler workers.
const QueueGroupCrawler = "crawler-workers"
