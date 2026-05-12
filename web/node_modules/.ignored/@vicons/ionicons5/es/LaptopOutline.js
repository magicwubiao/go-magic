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
    height: '304',
    rx: '32.14',
    ry: '32.14',
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
    stroke: 'currentColor',
    'stroke-linecap': 'round',
    'stroke-miterlimit': '10',
    'stroke-width': '32',
    d: 'M16 416h480',
    fill: 'currentColor'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_4 = [_hoisted_2, _hoisted_3]
export default defineComponent({
  name: 'LaptopOutline',
  render: function render(_ctx, _cache) {
    return _openBlock(), _createElementBlock('svg', _hoisted_1, _hoisted_4)
  }
})
