import { createElementVNode as _createElementVNode, openBlock as _openBlock, createElementBlock as _createElementBlock, defineComponent } from 'vue'
const _hoisted_1 = {
  xmlns: 'http://www.w3.org/2000/svg',
  'xmlns:xlink': 'http://www.w3.org/1999/xlink',
  viewBox: '0 0 512 512'
}
const _hoisted_2 = /*#__PURE__*/ _createElementVNode(
  'path',
  {
    d: 'M480 208H308L256 48l-52 160H32l140 96l-54 160l138-100l138 100l-54-160z',
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
    d: 'M256 48v316L118 464l54-160l-140-96h172l52-160z',
    fill: 'currentColor'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_4 = [_hoisted_2, _hoisted_3]
export default defineComponent({
  name: 'StarHalf',
  render: function render(_ctx, _cache) {
    return _openBlock(), _createElementBlock('svg', _hoisted_1, _hoisted_4)
  }
})
