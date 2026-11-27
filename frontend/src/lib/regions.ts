import names from "../../../internal/region/国家与地区.json";

const regionNames: Readonly<Record<string, string>> = names;
export const countryCodes = Object.keys(regionNames);

export function countryFlag(code: string) {
  return String.fromCodePoint(...[...code].map((character) => 0x1f1e6 + character.charCodeAt(0) - 65));
}

export function regionLabel(country?: string) {
  const code = country?.trim().toUpperCase() ?? "";
  const name = regionNames[code];
  return name ? countryFlag(code) + name : code;
}
