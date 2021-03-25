package seclient

import (
	"encoding/json"
	"errors"
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
