import { countryCodes } from "../../lib/regions";

export interface CountryOption {
  code: string;
  name: string;
  searchText: string;
}

const displayNames = new Intl.DisplayNames(["zh-CN"], { type: "region" });

export const countryOptions: CountryOption[] = countryCodes
  .map((code) => {
    const name = displayNames.of(code) ?? code;
    return { code, name, searchText: `${name} ${code}` };
  })
  .sort((left, right) => left.name.localeCompare(right.name, "zh-CN"));
