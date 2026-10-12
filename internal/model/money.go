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

// MilliPerYuan 一元是多少「厘」。
//
// 规格的单只价精确到厘（0.001 元 = 0.1 分）：整盒价摊到每只除不尽，
// 189 元 8 只 = 23.625 元/只，只有精确到厘，8 只才能正好算回 189。
// 厘只用在单价上，明细金额、订单金额仍然一律是整数「分」。
const MilliPerYuan = 1000

// MilliPerFen 1 分 = 10 厘。
const MilliPerFen = 10

// FormatMilliYuan 把「厘」格式化为元字符串，至少两位小数，第三位是 0 就省掉：
//
//	23625 -> "23.625"
//	35000 -> "35.00"
//	33600 -> "33.60"
func FormatMilliYuan(milli int64) string {
	neg := milli < 0
	if neg {
		milli = -milli
	}
	s := strconv.FormatInt(milli/MilliPerYuan, 10) + "."
	frac := milli % MilliPerYuan
	if frac%10 == 0 {
		s += pad2(frac / 10)
	} else {
		s += pad2(frac/10) + strconv.FormatInt(frac%10, 10)
	}
	if neg {
		s = "-" + s
	}
	return s
}

// LineAmount 一行明细的金额（分）= 只数 × 单只价，向上取整到**分**。
//
// 明细金额只是展示用的近似值，不做元的进位：每行各进一次位，
// 6 只母 3 两（209.25）+ 2 只公 4 两（69.75）会变成 210 + 70 = 280，
// 而同价的 8 只本该是 279。要收多少看 GoodsAmount，整单只进位一次。
func LineAmount(quantity int, unitPriceMilli int64) int64 {
	total := int64(quantity) * unitPriceMilli
	return (total + MilliPerFen - 1) / MilliPerFen
}

// GoodsAmount 货款（分）= Σ 只数 × 单只价，整单向上取整到元。
//
// 进位只在整单上做一次：8 只、16 只这种整盒的量本来就是整元（23.625 × 8 = 189），
// 混装凑满一盒也是整元；散买的零头进位：5 只 × 23.625 = 118.125 → 119 元。全整数运算。
func GoodsAmount(items []OrderItem) int64 {
	var total int64
	for _, it := range items {
		total += int64(it.Quantity) * it.UnitPriceMilli
	}
	return ceilYuan(total)
}

// QuoteAmount 单买一档 n 只要多少钱（分），即只有这一行的订单的货款。
// 价目表上「8 只 ¥189」的报价用它。
func QuoteAmount(quantity int, unitPriceMilli int64) int64 {
	return ceilYuan(int64(quantity) * unitPriceMilli)
}

// ceilYuan 厘 → 分，向上取整到元。
func ceilYuan(milli int64) int64 {
	yuan := (milli + MilliPerYuan - 1) / MilliPerYuan
	return yuan * 100
}
