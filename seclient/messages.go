package seclient

import (
	"encoding/json"
	"errors"
	"strconv"
)

type SEStatusPair struct {
	StatusCode int64
	Status     string
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
		StatusCode: code,
		Status: strStatus,
	}
	return nil
}
