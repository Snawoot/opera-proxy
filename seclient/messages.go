package seclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
)

const (
	SE_STATUS_OK int64 = 0
)

type SEStatusPair struct {
	Code    int64
	Message string
}

func (p *SEStatusPair) UnmarshalJSON(b []byte) error {
	var tmp map[string]string
	err := json.Unmarshal(b, &tmp)
	if err != nil {
		return err
	}

	if len(tmp) != 1 {
		return errors.New("ambiguous status")
	}

	var strCode, strStatus string
	for k, v := range tmp {
		strCode = k
		strStatus = v
	}

	code, err := strconv.ParseInt(strCode, 10, 64)
	if err != nil {
		return err
	}

	*p = SEStatusPair{
		Code:    code,
		Message: strStatus,
	}
	return nil
}

type SERegisterSubscriberResponse struct {
	Data   interface{}  `json:"data"`
	Status SEStatusPair `json:"return_code"`
}

type SERegisterDeviceData struct {
	ClientType     string `json:"client_type"`
	DeviceID       string `json:"device_id"`
	DevicePassword string `json:"device_password"`
}

type SERegisterDeviceResponse struct {
	Data   SERegisterDeviceData `json:"data"`
	Status SEStatusPair         `json:"return_code"`
}

type SEDeviceGeneratePasswordData struct {
	DevicePassword string `json:"device_password"`
}

type SEDeviceGeneratePasswordResponse struct {
	Data   SEDeviceGeneratePasswordData `json:"data"`
	Status SEStatusPair                 `json:"return_code"`
}

type SEGeoEntry struct {
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code"`
}

type SEGeoListResponse struct {
	Data struct {
		Geos []SEGeoEntry `json:"geos"`
	} `json:"data"`
	Status SEStatusPair `json:"return_code"`
}

type SEIPEntry struct {
	Geo   SEGeoEntry `json:"geo"`
	IP    string     `json:"ip"`
	Ports []uint16   `json:"ports"`
}

func (e *SEIPEntry) NetAddr() string {
	if len(e.Ports) == 0 {
		return net.JoinHostPort(e.IP, "443")
	} else {
		return net.JoinHostPort(e.IP, fmt.Sprintf("%d", e.Ports[0]))
	}
}

type SEDiscoverResponse struct {
	Data struct {
		IPs []SEIPEntry `json:"ips"`
	} `json:"data"`
	Status SEStatusPair `json:"return_code"`
}

type SESubscriberLoginResponse SERegisterSubscriberResponse
