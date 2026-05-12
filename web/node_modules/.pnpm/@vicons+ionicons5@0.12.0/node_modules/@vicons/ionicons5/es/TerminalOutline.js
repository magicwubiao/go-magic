import { createElementVNode as _createElementVNode, openBlock as _openBlock, createElementBlock as _createElementBlock, defineComponent } from 'vue'
const _hoisted_1 = {
  xmlns: 'http://www.w3.org/2000/svg',
  'xmlns:xlink': 'http://www.w3.org/1999/xlink',
  viewBox: '0 0 512 512'
}
const _hoisted_2 = /*#__PURE__*/ _createElementVNode(
  'rect',
  {
    x: '32',
    y: '48',
    width: '448',
    height: '416',
    rx: '48',
    ry: '48',
    fill: 'none',
    stroke: 'currentColor',
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
    fill: 'none',
    stroke: 'currentColor',
    'stroke-linecap': 'round',
    'stroke-linejoin': 'round',
    'stroke-width': '32',
    d: 'M96 112l80 64l-80 64'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_4 = /*#__PURE__*/ _createElementVNode(
  'path',
  {
    fill: 'none',
    stroke: 'currentColor',
    'stroke-linecap': 'round',
    'stroke-linejoin': 'round',
    'stroke-width': '32',
    d: 'M192 240h64'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_5 = [_hoisted_2, _hoisted_3, _hoisted_4]
export default defineComponent({
  name: 'TerminalOutline',
  render: function render(_ctx, _cache) {
    return _openBlock(), _createElementBlock('svg', _hoisted_1, _hoisted_5)
  }
})
