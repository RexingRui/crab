package model

import "strconv"

// 金额在整个系统中一律使用 int64 表示「分」。
// 绝对禁止用 float64/REAL 参与金额计算或存储，否则对账必然出现小数误差。

// FormatYuan 把「分」格式化为两位小数的「元」字符串，仅用于序列化输出。
//
//	0      -> "0.00"
//	88800  -> "888.00"
//	-50    -> "-0.50"
func FormatYuan(cents int64) string {
	neg := cents < 0
	abs := cents
	if neg {
		// 对 math.MinInt64 取负会溢出，业务上不可能出现，这里仍做保护。
		if abs == -9223372036854775808 {
			return "-92233720368547758.08"
		}
		abs = -abs
	}
	s := strconv.FormatInt(abs/100, 10) + "." + pad2(abs%100)
	if neg {
		s = "-" + s
	}
	return s
}

func pad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}
