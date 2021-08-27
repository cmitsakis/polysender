package android

type SettingLimitPerMinute uint32

func (s SettingLimitPerMinute) DBTable() string {
	return "settings"
}

func (s SettingLimitPerMinute) DBKey() []byte {
	return []byte("gateway.sms.android.limit_per_minute")
}

type SettingLimitPerHour uint32

func (s SettingLimitPerHour) DBTable() string {
	return "settings"
}

func (s SettingLimitPerHour) DBKey() []byte {
	return []byte("gateway.sms.android.limit_per_hour")
}

type SettingLimitPerDay uint32

func (s SettingLimitPerDay) DBTable() string {
	return "settings"
}

func (s SettingLimitPerDay) DBKey() []byte {
	return []byte("gateway.sms.android.limit_per_day")
}
