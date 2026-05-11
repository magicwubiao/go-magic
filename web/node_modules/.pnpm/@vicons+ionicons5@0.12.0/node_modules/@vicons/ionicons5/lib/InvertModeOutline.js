'use strict'
Object.defineProperty(exports, '__esModule', { value: true })
const vue_1 = require('vue')
const _hoisted_1 = {
  xmlns: 'http://www.w3.org/2000/svg',
  'xmlns:xlink': 'http://www.w3.org/1999/xlink',
  viewBox: '0 0 512 512'
}
const _hoisted_2 = /*#__PURE__*/ (0, vue_1.createElementVNode)(
  'circle',
  {
    fill: 'none',
    stroke: 'currentColor',
    'stroke-miterlimit': '10',
    'stroke-width': '32',
    cx: '256',
    cy: '256',
    r: '208'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_3 = /*#__PURE__*/ (0, vue_1.createElementVNode)(
  'path',
  {
    d: 'M256 176v160a80 80 0 0 1 0-160z',
    fill: 'currentColor'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_4 = /*#__PURE__*/ (0, vue_1.createElementVNode)(
  'path',
  {
    d: 'M256 48v128a80 80 0 0 1 0 160v128c114.88 0 208-93.12 208-208S370.88 48 256 48z',
    fill: 'currentColor'
  },
  null,
  -1
  /* HOISTED */
)
const _hoisted_5 = [_hoisted_2, _hoisted_3, _hoisted_4]
exports.default = (0, vue_1.defineComponent)({
  name: 'InvertModeOutline',
  render: function render(_ctx, _cache) {
    return (0, vue_1.openBlock)(), (0, vue_1.createElementBlock)('svg', _hoisted_1, _hoisted_5)
  }
})
