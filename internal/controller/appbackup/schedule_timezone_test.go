package appbackup

import "testing"

func TestVeleroScheduleExpression(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		want     string
	}{
		{
			name:     "standard cron uses beijing time",
			schedule: "0 0 * * *",
			want:     "CRON_TZ=Asia/Shanghai 0 0 * * *",
		},
		{
			name:     "descriptor cron uses beijing time",
			schedule: "@hourly",
			want:     "CRON_TZ=Asia/Shanghai @hourly",
		},
		{
			name:     "interval cron uses beijing time",
			schedule: "@every 5m",
			want:     "CRON_TZ=Asia/Shanghai @every 5m",
		},
		{
			name:     "existing cron tz is preserved",
			schedule: "CRON_TZ=UTC 0 0 * * *",
			want:     "CRON_TZ=UTC 0 0 * * *",
		},
		{
			name:     "legacy tz is preserved",
			schedule: "TZ=Asia/Tokyo @daily",
			want:     "TZ=Asia/Tokyo @daily",
		},
		{
			name:     "empty schedule is preserved",
			schedule: "   ",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := veleroScheduleExpression(tt.schedule); got != tt.want {
				t.Fatalf("veleroScheduleExpression(%q) = %q, want %q", tt.schedule, got, tt.want)
			}
		})
	}
}
