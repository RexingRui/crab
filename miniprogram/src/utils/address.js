/* 剪贴板地址解析 — 纯函数，无小程序 API 依赖，可直接在 node 下跑单测。
 *
 * 解析结果只是「建议」：调用方必须让用户点确认后才填入表单，绝不静默写入。 */

// 地名关键词：出现这些字的片段不可能是人名，同时也是判定地址的依据
export const PLACE_WORDS = ['省', '市', '区', '县', '镇', '乡', '街道', '路', '号', '栋', '幢', '室', '楼', '大道', '小区',
  '自治区', '街', '巷', '村', '组', '弄', '座', '单元', '层', '公寓', '花园', '广场', '大厦', 'industrial'];

// 表单标签词，连同其后的冒号一起去掉
const LABEL_WORDS = ['收货人', '收件人', '联系人', '姓名', '联系方式', '联系电话', '手机号码', '手机号', '手机',
  '电话', '详细地址', '所在地区', '地址'];

const SEPARATORS = /[\s,，、;；|]+/;

function stripDigitSeparators(text) {
  // 138-0013-8000 / 138 0013 8000 → 13800138000，只在数字之间去分隔符
  return text.replace(/(\d)[-\s](?=\d)/g, '$1');
}

function extractPhone(text) {
  const compact = stripDigitSeparators(text);
  const m = /1[3-9]\d{9}/.exec(compact);
  if (!m) return { phone: '', rest: text };

  // 在原文里定位这一段（可能带 - 或空格），逐字符走一遍把它抠掉
  const phone = m[0];
  let start = -1;
  let digits = 0;
  for (let i = 0; i < text.length && digits < phone.length; i++) {
    const ch = text[i];
    if (ch >= '0' && ch <= '9') {
      if (digits === 0) {
        if (ch !== phone[0]) continue;
        start = i;
      }
      if (ch !== phone[digits]) { digits = 0; start = -1; i = restartFrom(text, i); continue; }
      digits++;
    } else if (digits > 0 && (ch === '-' || ch === ' ')) {
      continue; // 号码内部的分隔符
    } else if (digits > 0) {
      digits = 0;
      start = -1;
    }
    if (digits === phone.length) {
      return { phone: phone, rest: text.slice(0, start) + ' ' + text.slice(i + 1), phoneAt: start };
    }
  }
  return { phone: phone, rest: compact.replace(phone, ' '), phoneAt: compact.indexOf(phone) };
}

function restartFrom(text, i) {
  return i - 1; // 让外层 for 的 i++ 把游标停在当前字符上重新起算
}

function stripLabels(text) {
  let out = text;
  LABEL_WORDS.forEach(function (w) {
    out = out.split(w + '：').join(' ');
    out = out.split(w + ':').join(' ');
    out = out.split(w).join(' ');
  });
  return out;
}

function hasPlaceWord(s) {
  for (let i = 0; i < PLACE_WORDS.length; i++) {
    if (s.indexOf(PLACE_WORDS[i]) >= 0) return true;
  }
  return false;
}

function isPlaceSuffix(ch) {
  return '省市区县镇乡街路号栋幢室楼村巷弄座层'.indexOf(ch) >= 0;
}

const CJK = /^[一-龥·]+$/;

function looksLikeName(s) {
  return s.length >= 2 && s.length <= 4 && CJK.test(s) && !hasPlaceWord(s);
}

/* 从「张三江苏省苏州市…」这种没有分隔符的整段里切出开头的人名 */
function leadingName(segment) {
  for (let len = 4; len >= 2; len--) {
    if (segment.length <= len) continue;
    const head = segment.slice(0, len);
    if (!CJK.test(head) || hasPlaceWord(head)) continue;
    if (isPlaceSuffix(segment[len])) continue; // 「江苏」后面跟着「省」，那是地名不是人名
    return head;
  }
  return '';
}

/**
 * 解析一段粘贴文本，返回 { name, phone, address }。
 * 任一项解析不出就留空字符串，交给用户手填，绝不猜。
 */
export function parseAddress(raw) {
  const empty = { name: '', phone: '', address: '' };
  if (!raw || typeof raw !== 'string') return empty;

  const text = raw.replace(/\r/g, '\n').trim();
  if (!text) return empty;

  const picked = extractPhone(text);
  const phone = picked.phone;
  const phoneAt = typeof picked.phoneAt === 'number' ? picked.phoneAt : -1;

  const cleaned = stripLabels(picked.rest);

  // 切片时记住每段在清洗后文本里的位置，用来判断它在号码之前还是之后
  const segments = [];
  let cursor = 0;
  cleaned.split(SEPARATORS).forEach(function (part) {
    const seg = part.trim();
    if (!seg) return;
    const at = cleaned.indexOf(seg, cursor);
    cursor = at >= 0 ? at + seg.length : cursor;
    segments.push({ text: seg, at: at >= 0 ? at : cursor });
  });

  // 地址：含地名关键词的最长一段
  let address = '';
  let addressSeg = null;
  segments.forEach(function (s) {
    if (hasPlaceWord(s.text) && s.text.length > address.length) {
      address = s.text;
      addressSeg = s;
    }
  });

  // 姓名：2-4 个连续汉字且不含地名词，优先号码之前、其次整段开头或结尾
  const candidates = segments.filter(function (s) {
    return s !== addressSeg && looksLikeName(s.text);
  });

  let name = '';
  if (candidates.length) {
    const beforePhone = phoneAt >= 0 ? candidates.filter(function (s) { return s.at < phoneAt; }) : [];
    const pool = beforePhone.length ? beforePhone : candidates;
    const first = pool[0];
    const last = pool[pool.length - 1];
    const head = segments.length ? segments[0] : null;
    const tail = segments.length ? segments[segments.length - 1] : null;
    if (head && pool.indexOf(head) >= 0) name = head.text;
    else if (tail && pool.indexOf(tail) >= 0) name = tail.text;
    else name = (first || last).text;
  } else if (address) {
    // 姓名和地址粘在一起没有分隔符
    const head = leadingName(address);
    if (head) {
      name = head;
      address = address.slice(head.length);
    }
  }

  return {
    name: name,
    phone: phone,
    address: address.replace(/^[\s,，、:：]+/, '').trim()
  };
}

/* 值不值得弹横幅：太短的、没有号码特征的一律不打扰 */
export function looksLikeAddressText(raw) {
  if (!raw || typeof raw !== 'string') return false;
  const text = raw.trim();
  if (text.length <= 15) return false;
  return /1[3-9]\d{9}/.test(stripDigitSeparators(text));
}
