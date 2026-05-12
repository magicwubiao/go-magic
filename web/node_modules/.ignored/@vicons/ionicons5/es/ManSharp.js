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
    cy: '56',
    r: '56',
    fill: 'currentColor'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_3 = /*#__PURE__*/ _createElementVNode(
  'path',
  {
    d: 'M336 128H176a32 32 0 0 0-32 32v160h48V192h8v320h52V328h8v184h52V192h8v128h48V160a32 32 0 0 0-32-32z',
    fill: 'currentColor'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_4 = [_hoisted_2, _hoisted_3]
export default defineComponent({
  name: 'ManSharp',
  render: function render(_ctx, _cache) {
    return _openBlock(), _createElementBlock('svg', _hoisted_1, _hoisted_4)
  }
})
