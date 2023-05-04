import string
from pycoingecko import CoinGeckoAPI
from collections import defaultdict

class PriceFetcher:
    coin_denom_mapping = {
        'cosmos': 'uatom',
        'osmosis': 'uosmo',
        'bitcoin': 'ubtc',
        'ethereum': 'ueth',
        'usd-coin': 'uusdc',
    }
    DELIMITER = ', '

    def __init__(self, api_key=None) -> None:
        self.cg = CoinGeckoAPI(api_key=api_key if api_key is not None else 'XXX')
        self.coin_prices = defaultdict(int)

    def create_price_feed(self, coin_list):
        price_feed = "1usei," # default 1 SEI to 1 USDC, will need to change once SEI price is available
        coin_price = self.cg.get_price(ids=coin_list, vs_currencies='usd')

        for coin in coin_list:
            price_feed += str(coin_price[coin]['usd']) + self.coin_denom_mapping[coin] + self.DELIMITER

        return price_feed.replace(" ", "")[:-1]



def test():
    pf = PriceFetcher()
    coin_list = ['cosmos', 'usd-coin']
    coin_prices = pf.create_price_feed(coin_list)
    print("coin_prices", coin_prices)

if __name__ == "__main__":
    test()
