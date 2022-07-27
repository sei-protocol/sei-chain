import string
from pycoingecko import CoinGeckoAPI
from collections import defaultdict

class PriceFetcher:
    coin_denom_mapping = {
        'cosmos': 'uatom',
        'osmosis': 'uosmo',
        'bitcoin': 'wbtc',
        'ethereum': 'weth',
    }
    DELIMITER = ', '

    def __init__(self) -> None:
        self.cg = cg = CoinGeckoAPI()
        self.coin_prices = defaultdict(int)

    def create_price_feed(self, coin_list):
        price_feed = ""
        usdc_conversion_rate = self.cg.get_price(ids='usd-coin', vs_currencies='usd')

        for coin in coin_list:
            coin_price = self.cg.get_price(ids=coin, vs_currencies='usd')
            price_feed += str(coin_price[coin]['usd']) + self.coin_denom_mapping[coin] + self.DELIMITER

        return price_feed.replace(" ", "")[:-1]



def test():
    pf = PriceFetcher()
    coin_list = ['cosmos', 'ethereum', 'bitcoin', 'osmosis']
    coin_prices = pf.create_price_feed(coin_list)
    print("coin_prices", coin_prices)

if __name__ == "__main__":
    test()