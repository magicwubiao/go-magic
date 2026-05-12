import { createElementVNode as _createElementVNode, openBlock as _openBlock, createElementBlock as _createElementBlock, defineComponent } from 'vue'
const _hoisted_1 = {
  xmlns: 'http://www.w3.org/2000/svg',
  'xmlns:xlink': 'http://www.w3.org/1999/xlink',
  viewBox: '0 0 512 512'
}
const _hoisted_2 = /*#__PURE__*/ _createElementVNode(
  'rect',
  {
    x: '48',
    y: '96',
    width: '416',
    height: '320',
    rx: '56',
    ry: '56',
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
    fill: 'none',
    stroke: 'currentColor',
    'stroke-linejoin': 'round',
    'stroke-width': '60',
    d: 'M48 192h416'
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
    'stroke-linejoin': 'round',
    'stroke-width': '60',
    d: 'M128 300h48v20h-48z'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_5 = [_hoisted_2, _hoisted_3, _hoisted_4]
export default defineComponent({
  name: 'CardOutline',
  render: function render(_ctx, _cache) {
    return _openBlock(), _createElementBlock('svg', _hoisted_1, _hoisted_5)
  }
})
