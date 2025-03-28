// Code generated by "enumer -type=AppealSortBy -trimprefix=AppealSortBy"; DO NOT EDIT.

package enum

import (
	"fmt"
	"strings"
)

const _AppealSortByName = "NewestOldestClaimed"

var _AppealSortByIndex = [...]uint8{0, 6, 12, 19}

const _AppealSortByLowerName = "newestoldestclaimed"

func (i AppealSortBy) String() string {
	if i < 0 || i >= AppealSortBy(len(_AppealSortByIndex)-1) {
		return fmt.Sprintf("AppealSortBy(%d)", i)
	}
	return _AppealSortByName[_AppealSortByIndex[i]:_AppealSortByIndex[i+1]]
}

// An "invalid array index" compiler error signifies that the constant values have changed.
// Re-run the stringer command to generate them again.
func _AppealSortByNoOp() {
	var x [1]struct{}
	_ = x[AppealSortByNewest-(0)]
	_ = x[AppealSortByOldest-(1)]
	_ = x[AppealSortByClaimed-(2)]
}

var _AppealSortByValues = []AppealSortBy{AppealSortByNewest, AppealSortByOldest, AppealSortByClaimed}

var _AppealSortByNameToValueMap = map[string]AppealSortBy{
	_AppealSortByName[0:6]:        AppealSortByNewest,
	_AppealSortByLowerName[0:6]:   AppealSortByNewest,
	_AppealSortByName[6:12]:       AppealSortByOldest,
	_AppealSortByLowerName[6:12]:  AppealSortByOldest,
	_AppealSortByName[12:19]:      AppealSortByClaimed,
	_AppealSortByLowerName[12:19]: AppealSortByClaimed,
}

var _AppealSortByNames = []string{
	_AppealSortByName[0:6],
	_AppealSortByName[6:12],
	_AppealSortByName[12:19],
}

// AppealSortByString retrieves an enum value from the enum constants string name.
// Throws an error if the param is not part of the enum.
func AppealSortByString(s string) (AppealSortBy, error) {
	if val, ok := _AppealSortByNameToValueMap[s]; ok {
		return val, nil
	}

	if val, ok := _AppealSortByNameToValueMap[strings.ToLower(s)]; ok {
		return val, nil
	}
	return 0, fmt.Errorf("%s does not belong to AppealSortBy values", s)
}

// AppealSortByValues returns all values of the enum
func AppealSortByValues() []AppealSortBy {
	return _AppealSortByValues
}

// AppealSortByStrings returns a slice of all String values of the enum
func AppealSortByStrings() []string {
	strs := make([]string, len(_AppealSortByNames))
	copy(strs, _AppealSortByNames)
	return strs
}

// IsAAppealSortBy returns "true" if the value is listed in the enum definition. "false" otherwise
func (i AppealSortBy) IsAAppealSortBy() bool {
	for _, v := range _AppealSortByValues {
		if i == v {
			return true
		}
	}
	return false
}
