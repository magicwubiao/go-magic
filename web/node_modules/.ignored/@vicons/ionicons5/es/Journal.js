import { createElementVNode as _createElementVNode, openBlock as _openBlock, createElementBlock as _createElementBlock, defineComponent } from 'vue'
const _hoisted_1 = {
  xmlns: 'http://www.w3.org/2000/svg',
  'xmlns:xlink': 'http://www.w3.org/1999/xlink',
  viewBox: '0 0 512 512'
}
const _hoisted_2 = /*#__PURE__*/ _createElementVNode(
  'path',
  {
    d: 'M290 32H144a64.07 64.07 0 0 0-64 64v320a64.07 64.07 0 0 0 64 64h146z',
    fill: 'currentColor'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_3 = /*#__PURE__*/ _createElementVNode(
  'path',
  {
    d: 'M368 32h-18v448h18a64.07 64.07 0 0 0 64-64V96a64.07 64.07 0 0 0-64-64z',
    fill: 'currentColor'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_4 = [_hoisted_2, _hoisted_3]
export default defineComponent({
  name: 'Journal',
  render: function render(_ctx, _cache) {
    return _openBlock(), _createElementBlock('svg', _hoisted_1, _hoisted_4)
  }
})
