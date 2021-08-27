package adb

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

func (d *Device) PreSend() error {
	// detect version
	versionFull, err := d.adbDevice.RunShellCommand("getprop", "ro.build.version.release")
	if err != nil {
		return fmt.Errorf("runAdbCommand failed: %w", err)
	}
	versionMajor := strings.SplitN(versionFull, ".", 2)[0]
	versionMajorInt, err := strconv.Atoi(versionMajor)
	if err != nil {
		return fmt.Errorf("strconv.Atoi failed: %w", err)
	}
	d.androidVersionMajor = versionMajorInt

	// detect service domain name
	servicesOutput, err := d.adbDevice.RunShellCommand("service", "list")
	if err != nil {
		return fmt.Errorf("runAdbCommand failed: %w", err)
	}
	var serviceDomainExpected string
	switch d.androidVersionMajor {
	case 5:
		serviceDomainExpected = "com.android.mms"
	case 6, 7:
		serviceDomainExpected = "com.android.mms"
	case 8, 9, 10:
		serviceDomainExpected = "com.android.mms.service"
	default:
		return fmt.Errorf("android version %d not supported", d.androidVersionMajor)
	}
	scanner := bufio.NewScanner(strings.NewReader(servicesOutput))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, ".IMms") || strings.Contains(line, "com.android.mms") {
			lineSplit := strings.Split(line, "[")
			if len(lineSplit) != 2 {
				continue
			}
			lastCharIndex := len(lineSplit[1]) - 1
			if lineSplit[1][lastCharIndex] != ']' {
				continue
			}
			if lastCharIndex < 2 {
				continue
			}
			serviceDomain := lineSplit[1][:lastCharIndex]
			if serviceDomainExpected != "" && serviceDomain == serviceDomainExpected {
				d.serviceDomain = serviceDomain
				break
			} else if strings.HasSuffix(serviceDomain, ".IMms") {
				d.serviceDomain = serviceDomain
			}
		}
	}
	if d.serviceDomain == "" {
		return fmt.Errorf("service domain not found")
	}
	if strings.Contains(d.serviceDomain, " ") {
		return fmt.Errorf("service domain contains white space")
	}
	return nil
}

func (d Device) SendSMS(to string, msg string) error {
	const subID = 1
	var err error
	switch d.androidVersionMajor {
	case 5:
		_, err = d.adbDevice.RunShellCommand("service", "call", "isms", "9", "s16", `"`+d.serviceDomain+`"`, "s16", `"`+to+`"`, "s16", "null", "s16", `"`+msg+`"`, "s16", "null", "s16", "null")
	case 6, 7:
		_, err = d.adbDevice.RunShellCommand("service", "call", "isms", "7", "i32", strconv.Itoa(subID), "s16", `"`+d.serviceDomain+`"`, "s16", `"`+to+`"`, "s16", "null", "s16", `"`+msg+`"`, "s16", "null", "s16", "null")
	case 8, 9, 10:
		_, err = d.adbDevice.RunShellCommand("service", "call", "isms", "7", "i32", strconv.Itoa(subID-1), "s16", `"`+d.serviceDomain+`"`, "s16", `"`+to+`"`, "s16", "null", "s16", `"`+msg+`"`, "s16", "null", "s16", "null")
	default:
		return fmt.Errorf("android version %d not supported", d.androidVersionMajor)
	}
	if err != nil {
		return fmt.Errorf("ADB shell command failed: %w", err)
	}
	return nil
}
