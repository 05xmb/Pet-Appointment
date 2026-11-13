const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

function loadPrototype() {
  const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");
  const start = html.indexOf("<script>");
  const end = html.indexOf("</script>");
  assert.ok(start >= 0 && end > start);

  const nodes = new Map();
  const document = {
    getElementById(id) {
      if (!nodes.has(id)) {
        nodes.set(id, { value: id === "book-slot" ? "1" : "", textContent: "", innerHTML: "", classList: { toggle() {} } });
      }
      return nodes.get(id);
    }
  };
  const context = vm.createContext({ document, prompt: () => "临时有事" });
  vm.runInContext(html.slice(start + "<script>".length, end), context);
  return context;
}

test("批准临近取消后创建 9900 分待退款记录，并向用户和客服显示金额", () => {
  const context = loadPrototype();
  vm.runInContext('book(); pay(1, "success"); advance(120); cancelBooking(1); reviewCancel(1, true);', context);

  const actual = vm.runInContext("({ status: state.bookings[0].status, refund: state.bookings[0].refund, amount: state.bookings[0].refundAmount })", context);
  assert.deepEqual({ ...actual }, { status: "CANCELLED", refund: "PENDING", amount: 9900 });
  assert.match(vm.runInContext("ownerView()", context), /退款：待退款 · ¥99\.00/);
  assert.match(vm.runInContext("supportView()", context), /待退款 · ¥99\.00/);
});

test("临近取消退款失败后重试成功，日清按实际退款额汇总", () => {
  const context = loadPrototype();
  vm.runInContext('book(); pay(1, "success"); advance(120); cancelBooking(1); reviewCancel(1, true); refundResult(1, false);', context);
  const failed = vm.runInContext("({ status: state.bookings[0].status, refund: state.bookings[0].refund, amount: state.bookings[0].refundAmount })", context);
  assert.deepEqual({ ...failed }, { status: "CANCELLED", refund: "FAILED", amount: 9900 });
  vm.runInContext("refundResult(1, true)", context);
  assert.match(vm.runInContext("ownerView()", context), /退款：退款成功 · ¥99\.00/);
  const daily = vm.runInContext("managerView()", context);
  assert.match(daily, /已收款<strong>¥99\.00/);
  assert.match(daily, /已退款<strong>¥99\.00/);
  assert.match(daily, /未退款<strong>¥0\.00/);
});

test("提前取消和关闭后迟到付款也记录全额待退", () => {
  const early = loadPrototype();
  vm.runInContext('book(); pay(1, "success"); cancelBooking(1);', early);
  assert.equal(vm.runInContext("state.bookings[0].refundAmount", early), 9900);

  const late = loadPrototype();
  vm.runInContext('book(); advance(5); pay(1, "success");', late);
  assert.equal(vm.runInContext("state.bookings[0].refundAmount", late), 9900);
});

test("时段开始后未核销可申请，待审禁核销，获批全额待退", () => {
  const context = loadPrototype();
  vm.runInContext('book(); pay(1, "success"); advance(180); cancelBooking(1);', context);
  const pending = vm.runInContext("({ status: state.bookings[0].status, reason: state.bookings[0].reason, occupied: occupied(1) })", context);
  assert.deepEqual({ ...pending }, { status: "CANCEL_REVIEW", reason: "临时有事", occupied: 1 });
  vm.runInContext("checkIn(1)", context);
  assert.equal(vm.runInContext("state.bookings[0].status", context), "CANCEL_REVIEW");
  vm.runInContext("reviewCancel(1, true)", context);
  const approved = vm.runInContext("({ status: state.bookings[0].status, refund: state.bookings[0].refund, amount: state.bookings[0].refundAmount, occupied: occupied(1) })", context);
  assert.deepEqual({ ...approved }, { status: "CANCELLED", refund: "PENDING", amount: 9900, occupied: 0 });
  assert.match(vm.runInContext("ownerView()", context), /退款：待退款 · ¥99\.00/);
});

test("时段结束后未标未到店仍可申请；已核销或已标未到店不可申请", () => {
  const context = loadPrototype();
  vm.runInContext('book(); pay(1, "success"); advance(241); cancelBooking(1);', context);
  assert.equal(vm.runInContext("state.bookings[0].status", context), "CANCEL_REVIEW");
  vm.runInContext("markNoShow(1)", context);
  assert.equal(vm.runInContext("state.bookings[0].status", context), "CANCEL_REVIEW");
  vm.runInContext("reviewCancel(1, false); markNoShow(1); cancelBooking(1)", context);
  assert.equal(vm.runInContext("state.bookings[0].status", context), "NO_SHOW");
  assert.equal(vm.runInContext("state.bookings[0].refund", context), "NONE");

  const checkedIn = loadPrototype();
  vm.runInContext('book(); pay(1, "success"); advance(180); checkIn(1); cancelBooking(1);', checkedIn);
  assert.equal(vm.runInContext("state.bookings[0].status", checkedIn), "CHECKED_IN");
  assert.equal(vm.runInContext("state.bookings[0].refund", checkedIn), "NONE");
});

test("时间展示和日清按 Asia/Shanghai 日期，不随浏览器时区变化", () => {
  const context = loadPrototype();
  assert.equal(vm.runInContext("localInput(new Date('2026-11-13T16:30:00Z'))", context), "2026-11-14T00:30");
  assert.match(vm.runInContext("stamp(new Date('2026-11-13T16:30:00Z'))", context), /2026\/11\/14 00:30:00/);
  vm.runInContext(`state.slots = [
    { id: 1, start: new Date('2026-11-13T16:30:00Z'), end: new Date('2026-11-13T17:30:00Z'), cap: 3 },
    { id: 2, start: new Date('2026-11-13T15:30:00Z'), end: new Date('2026-11-13T16:30:00Z'), cap: 3 }
  ]; state.day = '2026-11-14';`, context);
  const daily = vm.runInContext("managerView()", context);
  assert.match(daily, /<td>#1<br>/);
  assert.doesNotMatch(daily, /<td>#2<br>/);
  assert.match(daily, /Asia\/Shanghai/);
});

test("发布时段的日期时间输入按 Asia/Shanghai 解释为同一时刻", () => {
  const context = loadPrototype();
  vm.runInContext(`document.getElementById('slot-start').value = '2030-11-14T09:00';
    document.getElementById('slot-end').value = '2030-11-14T10:00'; publishSlot();`, context);
  assert.equal(vm.runInContext("state.slots.find(s => s.id === 4).start.toISOString()", context), "2030-11-14T01:00:00.000Z");
  assert.equal(vm.runInContext("state.slots.find(s => s.id === 4).end.toISOString()", context), "2030-11-14T02:00:00.000Z");
});
