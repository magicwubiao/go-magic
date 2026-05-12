import { createElementVNode as _createElementVNode, openBlock as _openBlock, createElementBlock as _createElementBlock, defineComponent } from 'vue'
const _hoisted_1 = {
  xmlns: 'http://www.w3.org/2000/svg',
  'xmlns:xlink': 'http://www.w3.org/1999/xlink',
  viewBox: '0 0 512 512'
}
const _hoisted_2 = /*#__PURE__*/ _createElementVNode(
  'circle',
  {
    cx: '256',
    cy: '96',
    r: '64',
    fill: 'none',
    stroke: 'currentColor',
    'stroke-linecap': 'round',
    'stroke-linejoin': 'round',
    'stroke-width': '32'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_3 = /*#__PURE__*/ _createElementVNode(
  'path',
  {
    d: 'M272 164a9 9 0 0 0-9-9h-14a9 9 0 0 0-9 9v293.56a32.09 32.09 0 0 0 2.49 12.38l10.07 24a3.92 3.92 0 0 0 6.88 0l10.07-24a32.09 32.09 0 0 0 2.49-12.38z',
    fill: 'currentColor'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_4 = /*#__PURE__*/ _createElementVNode(
  'circle',
  {
    cx: '280',
    cy: '72',
    r: '24',
    fill: 'currentColor'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_5 = [_hoisted_2, _hoisted_3, _hoisted_4]
export default defineComponent({
  name: 'PinOutline',
  render: function render(_ctx, _cache) {
    return _openBlock(), _createElementBlock('svg', _hoisted_1, _hoisted_5)
  }
})
