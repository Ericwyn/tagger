export const themeOptions = [
  {id: 'ivory', label: '象牙白', description: '明亮、克制，适合长时间整理曲库', swatch: ['#fffefa', '#e84b2c', '#191a17']},
  {id: 'sage', label: '灰绿色', description: '柔和的植物色，降低大面积白底的刺激', swatch: ['#f2f5ef', '#3f7568', '#1e2b27']},
  {id: 'terracotta', label: '陶土纸', description: '温暖的纸张与陶土色强调', swatch: ['#fbf1e7', '#b95738', '#2e211b']},
  {id: 'midnight', label: '午夜墨', description: '低亮度深色，适合夜间整理', swatch: ['#181b20', '#e47857', '#f1eee5']},
] as const;

export type ThemeID = typeof themeOptions[number]['id'];

export const fontOptions = [
  {id: 'editorial', label: '编辑体', description: '保留衬线标题与舒展的正文', sample: 'Archive / 曲库'},
  {id: 'clean', label: '清晰无衬线', description: '更直接的界面阅读节奏', sample: 'Archive / 曲库'},
  {id: 'humanist', label: '人文几何', description: '略带人文气质，适合长列表浏览', sample: 'Archive / 曲库'},
] as const;

export type FontID = typeof fontOptions[number]['id'];

export function normalizeTheme(value: string | null): ThemeID {
  if (value === 'dark') return 'midnight';
  return themeOptions.some((option) => option.id === value) ? value as ThemeID : 'ivory';
}

export function normalizeFont(value: string | null): FontID {
  return fontOptions.some((option) => option.id === value) ? value as FontID : 'editorial';
}
