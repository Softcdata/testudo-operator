package appbackup

import "strings"

const defaultVeleroScheduleTimeZone = "Asia/Shanghai"

func veleroScheduleExpression(schedule string) string {
	trimmed := strings.TrimSpace(schedule)
	if trimmed == "" {
		return ""
	}

	firstToken := strings.Fields(trimmed)[0]
	upperToken := strings.ToUpper(firstToken)
	if strings.HasPrefix(upperToken, "CRON_TZ=") || strings.HasPrefix(upperToken, "TZ=") {
		return trimmed
	}

	return "CRON_TZ=" + defaultVeleroScheduleTimeZone + " " + trimmed
}
