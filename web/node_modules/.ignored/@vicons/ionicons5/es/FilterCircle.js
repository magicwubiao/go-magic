import { createElementVNode as _createElementVNode, openBlock as _openBlock, createElementBlock as _createElementBlock, defineComponent } from 'vue'
const _hoisted_1 = {
  xmlns: 'http://www.w3.org/2000/svg',
  'xmlns:xlink': 'http://www.w3.org/1999/xlink',
  viewBox: '0 0 512 512'
}
const _hoisted_2 = /*#__PURE__*/ _createElementVNode(
  'path',
  {
    d: 'M256 48C141.31 48 48 141.31 48 256s93.31 208 208 208s208-93.31 208-208S370.69 48 256 48zm32 304h-64a16 16 0 0 1 0-32h64a16 16 0 0 1 0 32zm48-64H176a16 16 0 0 1 0-32h160a16 16 0 0 1 0 32zm32-64H144a16 16 0 0 1 0-32h224a16 16 0 0 1 0 32z',
    fill: 'currentColor'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_3 = [_hoisted_2]
export default defineComponent({
  name: 'FilterCircle',
  render: function render(_ctx, _cache) {
    return _openBlock(), _createElementBlock('svg', _hoisted_1, _hoisted_3)
  }
})
